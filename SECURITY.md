# Security Vulnerability Report — RocketChat/airlock

**Reporter:** _<your name / HackerOne handle>_
**Date:** 2026-10-27
**Component:** `RocketChat/airlock` (MongoDB-access operator for Rocket.Chat multi-tenancy)

---

## Summary

A **cross-namespace authorization bypass (confused-deputy)** in the airlock operator. A
low-privileged tenant who can only create a Kubernetes `Secret` in **their own namespace** can
force the operator to mutate the status of **other tenants'** `MongoDBAccessRequest` resources in
**other namespaces**, flipping them to `Ready=False / ClusterOutOfDate`. The operator performs a
privileged, cluster-wide write keyed on an attacker-controllable object **name**, without verifying
that the triggering principal is authorized over the affected namespace.

## Affected component & version

- Repository: `github.com/RocketChat/airlock`
- File: `controllers/mongodbcluster_controller.go`
- Tested build: Docker Hub image `rocketchat/airlock:1d0a231c6a868f2f47543f691b02a43d97d5e32d`
  (commit `1d0a231`, 2026-08-11) — the latest published image/commit at the time of testing.
- Deployment mode: **cluster-wide** — airlock's intended multi-tenant mode (one operator serving
  `MongoDBAccessRequest`s across many tenant namespaces).
- Please confirm the referenced lines still exist on `main`/latest before triage.

## Severity

**Medium** — cross-tenant integrity/availability. No confidentiality breach, no destructive delete.
Approximate CVSS 3.1: `AV:N/AC:L/PR:L/UI:N/S:C/C:N/I:L/A:L` (scope-changed: the effect crosses the
tenant/namespace trust boundary the operator is meant to enforce).

## Root cause

Two namespace-blind selections in the `MongoDBCluster` reconciler.

**(1) Secret → Cluster watch mapping — name-only, all namespaces.**
`findObjectsForSecret` (`controllers/mongodbcluster_controller.go:213`):

```go
listOps := &client.ListOptions{
    FieldSelector: fields.OneTermEqualSelector("connectionSecret", secret.GetName()), // NAME only
    Namespace:     "",                                                                // all namespaces
}
```

Any `Secret`, in **any** namespace, whose **name** equals some cluster's `spec.connectionSecret`
enqueues that (cluster-scoped, admin-owned) `MongoDBCluster` for reconcile — regardless of the
Secret's namespace or owner. The Secret's **contents are never used** for the match; only
`metadata.name` matters.

**(2) AccessRequest status update — all namespaces, filtered only by a logical key.**
`updateAccessRequests` (`:376`, called from `Reconcile` at `:209`):

```go
err := r.List(ctx, accessRequestList, []client.ListOption{
    // client.MatchingFields{"spec.clusterName": clusterName},   // <-- disabled
}...)                                                            // lists ALL ARs, in ALL namespaces
...
for _, accessRequest := range accessRequestList.Items {
    if accessRequest.Spec.ClusterName == clusterName {          // logical-key filter, NOT a namespace/ownership check
        meta.SetStatusCondition(... Ready=False, Reason=ClusterOutOfDate ...)
        r.Status().Update(ctx, &accessRequest)                  // :409  cross-namespace write
    }
}
```

The in-code comment at `:390–391` already documents the limitation:
> *"Also, with the current filter, this applies to ALL namespaces, but I don't have a way to get
> 'namespaces that reference this particular operator'."*

The `accessRequest.Spec.ClusterName == clusterName` check is a **logical-key** match, not a
namespace or ownership check, so the write crosses the namespace boundary.

`MongoDBAccessRequest` is a **namespaced** (tenant-owned) resource; `MongoDBCluster` is
**cluster-scoped** (admin-owned). The operator's ServiceAccount holds cluster-wide write on
`mongodbaccessrequests/status`, which makes it a confused deputy: it performs the cross-namespace
write on behalf of a principal (the Secret creator) who has no authorization over the target
namespace.

## Proof of Concept

> Reproduced on an **isolated kind cluster that I own** — not against RocketChat demo, community, or
> customer servers. All logs/captures come from my own installation, per your Disclosure Policy.

**Setup (administrator):**

1. Deploy airlock **cluster-wide** (image above) with a MongoDB backend whose admin user holds a
   user-admin role (`root` / `userAdminAnyDatabase` / `dbOwner@admin` / `userAdmin@admin`).
   `testMongoConnection` (`:309`) must pass, or `Reconcile` returns before the vulnerable sink.
2. Create the shared admin cluster and its connection secret:
   ```yaml
   apiVersion: airlock.cloud.rocket.chat/v1alpha1
   kind: MongoDBCluster
   metadata: { name: prod }
   spec:
     connectionSecret: prod-conn
     connectionSecretNamespace: airlock-system
     # ...connection fields for the backend above...
   ```
   plus the real `prod-conn` Secret in `airlock-system`.

**Victim (legitimate tenant, `ns-victim`):**

3. ```yaml
   apiVersion: airlock.cloud.rocket.chat/v1alpha1
   kind: MongoDBAccessRequest
   metadata: { name: victim-app, namespace: ns-victim }
   spec: { clusterName: prod, database: victimdb, userName: victim-app, secretName: victim-app-conn }
   ```
   The operator grants it (creates a Mongo user + writes a connection Secret into `ns-victim`) and
   sets `status.conditions[Ready]=True`. It then quiesces — no further writes to `ns-victim`.

**Attacker (low-privileged tenant, `ns-attacker` — needs only `create secrets` in their OWN namespace):**

4. ```bash
   kubectl -n ns-attacker create secret generic prod-conn \
       --from-literal=connectionString=mongodb://x:x@127.0.0.1:27017/x
   ```
   `prod-conn` equals the admin cluster's `spec.connectionSecret`. The attacker owns neither `prod`
   (cluster-scoped) nor `ns-victim`, and the Secret's contents are irrelevant — the **name** is the
   entire exploit.

**Observe the cross-tenant effect:**

5. ```bash
   kubectl -n ns-victim get mongodbaccessrequest victim-app -o jsonpath='{.status.conditions}'
   ```
   The victim's AccessRequest flips to `Ready=False / Reason=ClusterOutOfDate` — a write caused
   solely by the attacker's action in a **different** namespace. In the apiserver audit log, the
   operator ServiceAccount (`system:serviceaccount:airlock-system:airlock-controller-manager`)
   performs `update mongodbaccessrequests/status` on `ns-victim/victim-app`, with no corresponding
   action by the victim or any administrator.

I can provide a screen recording and the apiserver audit excerpt from my test cluster on request.

## Impact

In a multi-tenant airlock deployment (its intended use), **any** tenant able to create a Secret in
their own namespace can force **any** co-located tenant's `MongoDBAccessRequest` that shares the same
cluster to flip to `Ready=False / ClusterOutOfDate`. Any controller, automation, or operator-run
logic — and any human or CI gate — keying on that `Ready` condition is disrupted: this is a
cross-tenant **denial-of-service / integrity** issue on managed DB-access state. The attacker needs
only the cluster's `connectionSecret` **name**, which is low-entropy and typically guessable (or
directly readable if the tenant can `get` the cluster-scoped `MongoDBCluster`).

It is **not** secret disclosure and **not** a destructive delete.

## Suggested fix / mitigation

1. **Tether the Secret watch to the namespace.** In `findObjectsForSecret`, only enqueue a cluster
   when the Secret's namespace matches that cluster's `spec.ConnectionSecretNamespace` (i.e. compare
   `secret.GetNamespace()`), instead of `Namespace: ""` with a name-only field match.
2. **Tether the AccessRequest update.** In `updateAccessRequests`, restrict the List/Update to the
   namespace(s) legitimately bound to the cluster, or verify per-AR that its namespace is authorized
   for that cluster, rather than listing/updating **all** AccessRequests cluster-wide. If the field
   index on `spec.clusterName` is the blocker, a per-cluster namespace allowlist, or an
   ownership/authorization check before `Status().Update`, closes the boundary.
3. **Audit the operator for the same pattern** — any privileged action keyed on attacker-controllable
   name/metadata without a namespace or ownership check (both handlers above share this root cause).

## Disclosure & eligibility

- Discovered through independent static analysis plus a controlled behavioral test on an **isolated
  kind cluster I own**. No testing was performed against RocketChat demo, community, or customer
  servers; all screen captures/logs come from my own installation (per your Disclosure Policy).
- To the best of my knowledge I am the first reporter of this issue; I discovered it personally; I am
  not, and have not within the last 12 months been, employed by RocketChat or any related entity.
- Please allow a reasonable remediation window before any public disclosure. I'm glad to coordinate
  timing and to re-test a candidate patch.
