package conditions

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/RocketChat/airlock/api/v1alpha1"
	"github.com/RocketChat/airlock/internal/rules"
)

type StatusManager struct {
	conditions   *[]metav1.Condition
	object       v1alpha1.Object2
	phaseRules   []rules.PhaseRule
	statusClient client.StatusClient
}

// we only set status of objects we own, therefore justified to use a different interface than client.Object
// which means we miss out on core resources
func NewManager(statusClient client.StatusClient, conditions *[]metav1.Condition, object v1alpha1.Object2, rules []rules.PhaseRule) *StatusManager {
	return &StatusManager{
		conditions:   conditions,
		object:       object,
		phaseRules:   rules,
		statusClient: statusClient,
	}
}

type Condition struct {
	Type    string
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}

func (m *StatusManager) SetConditions(ctx context.Context, conditions []Condition) error {
	logger := log.FromContext(ctx)

	base := m.object.DeepCopyObject().(client.Object)

	changed := false

	for _, condition := range conditions {
		changed = meta.SetStatusCondition(m.conditions, metav1.Condition{
			Type:               condition.Type,
			Status:             condition.Status,
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: m.object.GetGeneration(),
		})

		if changed {
			logger.Info("status condition updated", "condition", condition.Type, "status", condition.Status, "reason", condition.Reason, "message", condition.Message, "phase", m.object.GetPhase())
		}
	}

	if changed {
		ruleMatched := false

		// recompute phase, since a condition status has changed
		for _, rule := range m.phaseRules {
			if rule.Satisfies(*m.conditions) {
				m.object.SetPhase(rule.Phase())
				ruleMatched = true
				break
			}
		}

		if !ruleMatched {
			m.object.SetPhase(rules.PhaseUnknown)
		}

		// mark as spec observed and processed
		m.object.SetObservedGeneration(m.object.GetGeneration())

		return m.statusClient.Status().Patch(ctx, m.object, client.MergeFrom(base))
	}

	return nil
}

func (m *StatusManager) SetCondition(ctx context.Context, conditionType string, status metav1.ConditionStatus, reason, message string) error {
	logger := log.FromContext(ctx)

	/*
	* https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client#Object
	* For example, nearly all the built-in types are Objects, as well as all KubeBuilder-generated CRDs (unless you do something real funky to them).
	* By and large, most things that implement runtime.Object also implement Object -- it's very rare to have *just* a runtime.Object implementation (the cases tend to be funky built-in types like Webhook payloads that don't have a `metadata` field).
	 */
	base := m.object.DeepCopyObject().(client.Object)

	if meta.SetStatusCondition(m.conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: m.object.GetGeneration(),
	}) {
		ruleMatched := false

		// recompute phase, since a condition status has changed
		for _, rule := range m.phaseRules {
			if rule.Satisfies(*m.conditions) {
				m.object.SetPhase(rule.Phase())
				ruleMatched = true
				break
			}
		}

		if !ruleMatched {
			m.object.SetPhase(rules.PhaseUnknown)
		}

		// mark as spec observed and processed
		m.object.SetObservedGeneration(m.object.GetGeneration())

		logger.Info("status condition updated", "condition", conditionType, "status", status, "reason", reason, "message", message, "phase", m.object.GetPhase())

		return m.statusClient.Status().Patch(ctx, m.object, client.MergeFrom(base))
	}

	return nil
}
