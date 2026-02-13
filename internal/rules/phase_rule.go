package rules

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const PhaseUnknown = "Unknown"

type PhaseRule interface {
	// Satisfies returns true if the conditions satisfy the rule for this phase
	Satisfies(conditions []metav1.Condition) bool

	// Phase returns the phase the rule satisfied
	Phase() string

	// ComputePhase checks if satisfies the rule, if not, return Unknown
	ComputePhase(conditions []metav1.Condition) string
}

type phaseRuleAll struct {
	phase string

	conditions map[string][]metav1.ConditionStatus
}

var _ PhaseRule = (*phaseRuleAll)(nil)

type phaseRuleAny struct {
	phase string

	conditions map[string][]metav1.ConditionStatus
}

var _ PhaseRule = (*phaseRuleAny)(nil)

type conditionMatcher struct {
	matchers []ConditionEqualsMatcher
	all      bool
}

type ConditionEqualsMatcher func() (condition string, status metav1.ConditionStatus)

func conditions(all bool, matcherLists ...[]ConditionEqualsMatcher) conditionMatcher {
	finalMatcher := make([]ConditionEqualsMatcher, 0, len(matcherLists))

	for _, matchers := range matcherLists {
		finalMatcher = append(finalMatcher, matchers...)
	}

	return conditionMatcher{
		matchers: finalMatcher,
		all:      all,
	}
}

func ConditionsAll(matchers ...[]ConditionEqualsMatcher) conditionMatcher {
	return conditions(true, matchers...)
}

func ConditionsAny(matchers ...[]ConditionEqualsMatcher) conditionMatcher {
	return conditions(false, matchers...)
}

// equals any one of
func ConditionEquals(condition string, statuses ...metav1.ConditionStatus) []ConditionEqualsMatcher {
	matchers := make([]ConditionEqualsMatcher, len(statuses))

	for i, status := range statuses {
		s := status
		matchers[i] = func() (string, metav1.ConditionStatus) {
			return condition, s
		}
	}

	return matchers
}

func NewPhaseRule(phase string, matcher conditionMatcher) PhaseRule {
	var conditionToStatusMap = make(map[string][]metav1.ConditionStatus, len(matcher.matchers))

	for _, matcher := range matcher.matchers {
		condition, status := matcher()
		// initialize the slice
		if conditionToStatusMap[condition] == nil {
			conditionToStatusMap[condition] = make([]metav1.ConditionStatus, 0) // True, False, Unknown
		}

		conditionToStatusMap[condition] = append(conditionToStatusMap[condition], status)
	}

	if matcher.all {
		return &phaseRuleAll{
			phase:      phase,
			conditions: conditionToStatusMap,
		}
	}

	return &phaseRuleAny{
		phase:      phase,
		conditions: conditionToStatusMap,
	}
}

func (r *phaseRuleAll) Satisfies(conditions []metav1.Condition) bool {
	var currentConditionToStatusMap = make(map[string]metav1.ConditionStatus, len(conditions))

	for _, condition := range conditions {
		currentConditionToStatusMap[condition.Type] = condition.Status
	}

	for requiredConditionType, requiredConditionStatus := range r.conditions {
		if currentStatus, exists := currentConditionToStatusMap[requiredConditionType]; exists {
			// required tyope exists in current state
			// but if the status in state does not match any of the required statuses, does not satisfy
			if !slices.Contains(requiredConditionStatus, currentStatus) {
				return false
			}
		} else {
			// required condition by rule, is not present in the condition list of the resource
			// does not satisfy
			return false
		}
	}

	// if all required conditions are present and equal, it satisfies
	return true
}

func (r *phaseRuleAll) Phase() string {
	return r.phase
}

func (r *phaseRuleAll) ComputePhase(conditions []metav1.Condition) string {
	if r.Satisfies(conditions) {
		return r.Phase()
	}

	return PhaseUnknown
}

func (r *phaseRuleAny) Satisfies(conditions []metav1.Condition) bool {
	// Any rule dictates that at least one of the required conditions are present and is equal to one of the statuses required by the rule

	for _, condition := range conditions {
		if statuses, exists := r.conditions[condition.Type]; exists {
			if slices.Contains(statuses, condition.Status) {
				return true
			}

			// return false
			// ^ since any means any of the conditions can be met, we allow iterating until we find potentially one that satisfies
		}
	}

	// among all the conditions in the current state, if none is required by the rule to satisfy, allow
	// if current conditions do not have the ones that the rule requires, it does not satisfy
	return false
}

func (r *phaseRuleAny) Phase() string {
	return r.phase
}

func (r *phaseRuleAny) ComputePhase(conditions []metav1.Condition) string {
	if r.Satisfies(conditions) {
		return r.Phase()
	}

	return PhaseUnknown
}
