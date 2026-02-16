package rules

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/RocketChat/airlock/internal/sets"
)

const PhaseUnknown = "Unknown"

type PhaseRule interface {
	// Satisfies returns true if the conditions satisfy the rule for this phase
	Satisfies(conditions *[]metav1.Condition) bool

	// Phase returns the phase the rule satisfied
	Phase() string

	// ComputePhase checks if satisfies the rule, if not, return Unknown
	ComputePhase(conditions *[]metav1.Condition) string
}

// ConditionMatcher matches a condition against a set of expected statuses
// those statuses are, as of now, True, False and Unknown
type ConditionMatcher interface {
	// Match returns true if the (type, status) tuple matches one of the expected tuples
	Matches(conditions *[]metav1.Condition) bool

	// ConditionType returns the unerlying condition that this matcher is for
	ConditionTypes() sets.Set[string]
}

type conditionEqualsMatcher struct {
	condition string
	statuses  []metav1.ConditionStatus
}

var _ ConditionMatcher = (*conditionEqualsMatcher)(nil)

func (m *conditionEqualsMatcher) Matches(conditions *[]metav1.Condition) bool {
	if conditions == nil {
		return false
	}

	for _, condition := range *conditions {
		if condition.Type == m.condition && slices.Contains(m.statuses, condition.Status) {
			return true
		}
	}

	return false
}

func (m *conditionEqualsMatcher) ConditionTypes() sets.Set[string] {
	return sets.New(m.condition)
}

// ConditionEquals returns matchers for a condition type that may equal any one of the given statuses.
func ConditionEquals(condition string, statuses ...metav1.ConditionStatus) ConditionMatcher {
	return &conditionEqualsMatcher{
		condition: condition,
		statuses:  statuses,
	}
}

type conditionMatcherAll struct {
	// a condition must match all the matcherReferences
	matcherReferences []ConditionMatcher
}

var _ ConditionMatcher = (*conditionMatcherAll)(nil)

func (m *conditionMatcherAll) Matches(conditions *[]metav1.Condition) bool {
	if conditions == nil {
		return false
	}

	for _, matcher := range m.matcherReferences {
		if !matcher.Matches(conditions) {
			return false
		}
	}

	return true
}

func (m *conditionMatcherAll) ConditionTypes() sets.Set[string] {
	types := sets.New[string]()

	for _, matcher := range m.matcherReferences {
		types.DestructiveUnion(matcher.ConditionTypes())
	}

	return types
}

func ConditionsAll(matchers ...ConditionMatcher) ConditionMatcher {
	return &conditionMatcherAll{
		matcherReferences: matchers,
	}
}

type conditionMatcherAny struct {
	// a condition must match at least one of the matcherReferences
	matcherReferences []ConditionMatcher
}

var _ ConditionMatcher = (*conditionMatcherAny)(nil)

func (m *conditionMatcherAny) Matches(conditions *[]metav1.Condition) bool {
	if conditions == nil {
		return false
	}

	for _, matcher := range m.matcherReferences {
		if matcher.Matches(conditions) {
			return true
		}
	}

	return false
}

func (m *conditionMatcherAny) ConditionTypes() sets.Set[string] {
	types := sets.New[string]()

	for _, matcher := range m.matcherReferences {
		types.DestructiveUnion(matcher.ConditionTypes())
	}

	return types
}

func ConditionsAny(matchers ...ConditionMatcher) ConditionMatcher {
	return &conditionMatcherAny{
		matcherReferences: matchers,
	}
}

type phaseRuleSimple struct {
	phase   string
	matcher ConditionMatcher
}

var _ PhaseRule = (*phaseRuleSimple)(nil)

func NewPhaseRule(phase string, matcher ConditionMatcher) PhaseRule {
	return &phaseRuleSimple{
		phase:   phase,
		matcher: matcher,
	}
}

func (r *phaseRuleSimple) Satisfies(conditions *[]metav1.Condition) bool {
	if conditions == nil {
		return false
	}

	conditionSet := sets.New[string]()

	for _, condition := range *conditions {
		conditionSet.Insert(condition.Type)
	}

	domainConditions := r.matcher.ConditionTypes()

	stateConditions := *conditions

	for domainCondition := range domainConditions {
		if conditionSet.Has(domainCondition) {
			continue
		}

		stateConditions = append(stateConditions, metav1.Condition{
			Type:   domainCondition,
			Status: metav1.ConditionUnknown,
		}) // don't care for the other fields
	}

	return r.matcher.Matches(&stateConditions)
}

func (r *phaseRuleSimple) Phase() string {
	return r.phase
}

func (r *phaseRuleSimple) ComputePhase(conditions *[]metav1.Condition) string {
	if r.Satisfies(conditions) {
		return r.Phase()
	}

	return PhaseUnknown
}
