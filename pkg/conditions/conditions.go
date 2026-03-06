package conditions

import (
	"context"

	"github.com/RocketChat/airlock/pkg/webhook"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ConditionsManager struct {
	conditions   *[]metav1.Condition
	object       client.Object
	statusClient client.StatusClient
	// fills later since do not need to clone unless needed
	baseObject client.Object
	webhook    *webhook.Manager
}

// we only set status of objects we own, therefore justified to use a different interface than client.Object
// which means we miss out on core resources
func NewManager(statusClient client.StatusClient, object client.Object, conditions *[]metav1.Condition, webhook *webhook.Manager) *ConditionsManager {
	return &ConditionsManager{
		conditions:   conditions,
		object:       object,
		statusClient: statusClient,
		webhook:      webhook,
	}
}

type condition struct {
	conditionType   string
	conditionStatus metav1.ConditionStatus
	reason          string
	message         string
}

func NewCondition(conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) condition {
	return condition{
		conditionType:   conditionType,
		conditionStatus: conditionStatus,
		reason:          reason,
		message:         message,
	}
}

// setCondition sets a condition but doens't patch it, returns if condition changed or not
func (m *ConditionsManager) setCondition(conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) (changed bool) {
	// switches if transitioning
	changed = false

	if m.conditions == nil {
		return
	}

	newCondition := metav1.Condition{
		Type:               conditionType,
		Status:             conditionStatus,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: m.object.GetGeneration(),
	}

	existingCondition := meta.FindStatusCondition(*m.conditions, conditionType)
	if existingCondition == nil {
		changed = true
		m.deepCopyBaseObjectOnce()
		*m.conditions = append(*m.conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		changed = true
		m.deepCopyBaseObjectOnce()
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = newCondition.LastTransitionTime
	}
	if existingCondition.Reason != newCondition.Reason {
		changed = true
		m.deepCopyBaseObjectOnce()
		existingCondition.Reason = newCondition.Reason
	}
	if existingCondition.Message != newCondition.Message {
		changed = true
		m.deepCopyBaseObjectOnce()
		existingCondition.Message = newCondition.Message
	}
	if existingCondition.ObservedGeneration != newCondition.ObservedGeneration {
		changed = true
		m.deepCopyBaseObjectOnce()
		existingCondition.ObservedGeneration = newCondition.ObservedGeneration
	}

	return
}

func (m *ConditionsManager) SetConditions(ctx context.Context, conditions ...condition) (changed bool, err error) {
	for _, condition := range conditions {
		changed = changed || m.setCondition(condition.conditionType, condition.conditionStatus, condition.reason, condition.message)
	}

	if changed {
		err = m.patchStatus(ctx)
		if err != nil {
			return
		}
	}

	return
}

func (m *ConditionsManager) SetCondition(ctx context.Context, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) (changed bool, err error) {
	logger := log.FromContext(ctx)

	changed = m.setCondition(conditionType, conditionStatus, reason, message)

	if changed {
		logger.Info("status condition updated", "condition", conditionType, "status", conditionStatus, "reason", reason, "message", message)

		go func() {
			group := m.object.GetObjectKind().GroupVersionKind().Group
			kind := m.object.GetObjectKind().GroupVersionKind().Kind
			condition := conditionType
			status := string(conditionStatus)
			if err := m.webhook.Send(ctx, group, kind, condition, status); err != nil {
				logger.Error(err, "failed to send webhook")
			}
		}()

		err = m.patchStatus(ctx)

		return
	}

	return
}

func (m *ConditionsManager) deepCopyBaseObjectOnce() {
	if m.baseObject != nil {
		return
	}

	m.baseObject = deepCopy(m.object)
}

func (m *ConditionsManager) patchStatus(ctx context.Context) error {
	err := m.statusClient.Status().Patch(ctx, m.object, client.MergeFrom(m.baseObject))
	if err != nil {
		// if errored, don't reset
		return err
	}

	m.baseObject = nil // reset

	return nil
}

func deepCopy(object client.Object) client.Object {
	/*
	* https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client#Object
	* For example, nearly all the built-in types are Objects, as well as all KubeBuilder-generated CRDs (unless you do something real funky to them).
	* By and large, most things that implement runtime.Object also implement Object -- it's very rare to have *just* a runtime.Object implementation (the cases tend to be funky built-in types like Webhook payloads that don't have a `metadata` field).
	 */
	return object.DeepCopyObject().(client.Object)
}

func (m *ConditionsManager) IsConditionTrueAndValid(conditionType string) bool {
	if m.conditions == nil {
		return false
	}

	condition := meta.FindStatusCondition(*m.conditions, conditionType)
	if condition == nil {
		return false
	}

	return condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == m.object.GetGeneration()
}
