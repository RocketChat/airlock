package rules

// thansk cursor

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func cond(ctype string, status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{Type: ctype, Status: status}
}

// ---- ContainsAll (ConditionsAll) ----

func TestPhaseRuleAll_Satisfies_EmptyConditions_RequiresOne(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	if rule.Satisfies(nil) {
		t.Error("expected false when conditions is nil")
	}
	if rule.Satisfies(&[]metav1.Condition{}) {
		t.Error("expected false when conditions is empty")
	}
}

func TestPhaseRuleAll_Satisfies_OneCondition_Matching(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if !rule.Satisfies(&conds) {
		t.Error("expected true when single required condition matches")
	}
}

func TestPhaseRuleAll_Satisfies_OneCondition_WrongStatus(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionFalse)}
	if rule.Satisfies(&conds) {
		t.Error("expected false when status does not match")
	}
	conds[0].Status = metav1.ConditionUnknown
	if rule.Satisfies(&conds) {
		t.Error("expected false when status is Unknown")
	}
}

func TestPhaseRuleAll_Satisfies_OneCondition_Missing(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("B", metav1.ConditionTrue)}
	if rule.Satisfies(&conds) {
		t.Error("expected false when required condition type is missing")
	}
}

func TestPhaseRuleAll_Satisfies_MultipleConditions_AllMatching(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionTrue),
		ConditionEquals("C", metav1.ConditionFalse),
	))
	conds := []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("B", metav1.ConditionTrue),
		cond("C", metav1.ConditionFalse),
	}
	if !rule.Satisfies(&conds) {
		t.Error("expected true when all required conditions match")
	}
}

func TestPhaseRuleAll_Satisfies_MultipleConditions_OneMissing(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if rule.Satisfies(&conds) {
		t.Error("expected false when one required condition is missing, condition B should be considered Unknown")
	}
}

func TestPhaseRuleAll_Satisfies_MultipleConditions_OneWrongStatus(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("B", metav1.ConditionFalse),
	}
	if rule.Satisfies(&conds) {
		t.Error("expected false when one condition has wrong status")
	}
}

func TestPhaseRuleAll_Satisfies_AllowedStatuses_OneMatches(t *testing.T) {
	// Same condition type, multiple allowed statuses (True or Unknown)
	rule := NewPhaseRule("Pending", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue, metav1.ConditionUnknown),
	))
	if !rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
		t.Error("expected true when condition is True")
	}
	if !rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionUnknown)}) {
		t.Error("expected true when condition is Unknown")
	}
	if rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionFalse)}) {
		t.Error("expected false when condition is False (not in allowed list)")
	}
}

func TestPhaseRuleAll_Satisfies_ExtraConditions_Ignored(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("B", metav1.ConditionFalse),
		cond("C", metav1.ConditionUnknown),
	}
	if !rule.Satisfies(&conds) {
		t.Error("expected true; extra conditions should not affect match")
	}
}

func TestPhaseRuleAll_Satisfies_EmptyRule(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll())
	if !rule.Satisfies(&[]metav1.Condition{}) {
		t.Error("empty All rule should satisfy any conditions (no requirements)")
	}
	if !rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
		t.Error("empty All rule should satisfy any conditions")
	}
}

func TestPhaseRuleAll_Phase(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	if got := rule.Phase(); got != "Ready" {
		t.Errorf("Phase() = %q, want %q", got, "Ready")
	}
}

func TestPhaseRuleAll_ComputePhase_Satisfied(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if got := rule.ComputePhase(&conds); got != "Ready" {
		t.Errorf("ComputePhase() = %q, want %q", got, "Ready")
	}
}

func TestPhaseRuleAll_ComputePhase_NotSatisfied(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionFalse)}
	if got := rule.ComputePhase(&conds); got != PhaseUnknown {
		t.Errorf("ComputePhase() = %q, want %q", got, PhaseUnknown)
	}
}

// ---- ContainsAny (ConditionsAny) ----

func TestPhaseRuleAny_Satisfies_EmptyConditions_RequiresOne(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	if rule.Satisfies(nil) {
		t.Error("expected false when conditions is nil")
	}
	if rule.Satisfies(&[]metav1.Condition{}) {
		t.Error("expected false when conditions is empty")
	}
}

func TestPhaseRuleAny_Satisfies_OneCondition_Matching(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if !rule.Satisfies(&conds) {
		t.Error("expected true when single condition matches")
	}
}

func TestPhaseRuleAny_Satisfies_OneCondition_WrongStatus(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionFalse)}
	if rule.Satisfies(&conds) {
		t.Error("expected false when status does not match")
	}
}

func TestPhaseRuleAny_Satisfies_OneCondition_Missing(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("B", metav1.ConditionTrue)}
	if rule.Satisfies(&conds) {
		t.Error("expected false when required condition is missing")
	}
}

func TestPhaseRuleAny_Satisfies_MultipleConditions_OneMatches(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionTrue),
		ConditionEquals("C", metav1.ConditionTrue),
	))
	// Only A matches
	conds := []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("B", metav1.ConditionFalse),
		cond("C", metav1.ConditionFalse),
	}
	if !rule.Satisfies(&conds) {
		t.Error("expected true when at least one condition matches")
	}
}

func TestPhaseRuleAny_Satisfies_MultipleConditions_NoneMatch(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{
		cond("A", metav1.ConditionFalse),
		cond("B", metav1.ConditionFalse),
	}
	if rule.Satisfies(&conds) {
		t.Error("expected false when no condition matches")
	}
}

func TestPhaseRuleAny_Satisfies_MultipleConditions_AllMatch(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("B", metav1.ConditionTrue),
	}
	if !rule.Satisfies(&conds) {
		t.Error("expected true when all conditions match (any still satisfies)")
	}
}

func TestPhaseRuleAny_Satisfies_AllowedStatuses_OneMatches(t *testing.T) {
	rule := NewPhaseRule("Pending", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue, metav1.ConditionUnknown),
	))
	if !rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
		t.Error("expected true when condition is True")
	}
	if !rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionUnknown)}) {
		t.Error("expected true when condition is Unknown")
	}
	if rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionFalse)}) {
		t.Error("expected false when condition is False")
	}
}

func TestPhaseRuleAny_Satisfies_EmptyRule(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny())
	if rule.Satisfies(nil) {
		t.Error("empty Any rule with nil conditions should not satisfy")
	}
	if rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
		t.Error("empty Any rule has no required conditions, so nothing to match")
	}
}

func TestPhaseRuleAny_Phase(t *testing.T) {
	rule := NewPhaseRule("Failed", ConditionsAny(
		ConditionEquals("A", metav1.ConditionFalse),
	))
	if got := rule.Phase(); got != "Failed" {
		t.Errorf("Phase() = %q, want %q", got, "Failed")
	}
}

func TestPhaseRuleAny_ComputePhase_Satisfied(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if got := rule.ComputePhase(&conds); got != "Ready" {
		t.Errorf("ComputePhase() = %q, want %q", got, "Ready")
	}
}

func TestPhaseRuleAny_ComputePhase_NotSatisfied(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionFalse)}
	if got := rule.ComputePhase(&conds); got != PhaseUnknown {
		t.Errorf("ComputePhase() = %q, want %q", got, PhaseUnknown)
	}
}

// ---- ConditionEquals multiple statuses ----

func TestConditionEquals_MultipleStatuses_AllReturnedCorrectly(t *testing.T) {
	matcher := ConditionEquals("X", metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown)
	types := matcher.ConditionTypes()
	if !types.Has("X") || types.Len() != 1 {
		t.Errorf("ConditionTypes() = %v, want set containing only X", types)
	}
	for _, status := range []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown} {
		c := &metav1.Condition{Type: "X", Status: status}
		if matcher.Matches(&[]metav1.Condition{*c}) != true {
			t.Errorf("expected MatcherMatched for status %v", status)
		}
	}
	// False status not in allowed list (we only have True, False, Unknown - so any other would fail; use a wrong type)
	wrongType := &metav1.Condition{Type: "Y", Status: metav1.ConditionTrue}
	if matcher.Matches(&[]metav1.Condition{*wrongType}) != false {
		t.Error("expected MatcherNotMatched for wrong condition type")
	}
}

// ---- ConditionsAll with multiple matcher groups ----

func TestConditionsAll_MultipleMatcherGroups(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionFalse),
	))
	conds := []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("B", metav1.ConditionFalse),
	}
	if !rule.Satisfies(&conds) {
		t.Error("expected true when all condition groups match")
	}
}

// ---- ConditionsAny with multiple matcher groups ----

func TestConditionsAny_MultipleMatcherGroups(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionTrue),
	))
	// Only A matches
	condsA := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if !rule.Satisfies(&condsA) {
		t.Error("expected true when first group matches")
	}
	// Only B matches
	condsB := []metav1.Condition{cond("B", metav1.ConditionTrue)}
	if !rule.Satisfies(&condsB) {
		t.Error("expected true when second group matches")
	}
	// Neither matches
	condsNone := []metav1.Condition{
		cond("A", metav1.ConditionFalse),
		cond("B", metav1.ConditionFalse),
	}
	if rule.Satisfies(&condsNone) {
		t.Error("expected false when no group matches")
	}
}

// nested / recursive matchers
func TestNested_ConditionsAllConditionsAny(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		// this
		ConditionsAny(ConditionEquals("A", metav1.ConditionTrue), ConditionEquals("B", metav1.ConditionTrue)),
		// and this
		ConditionEquals("C", metav1.ConditionTrue),
	))

	// A true, but C also needs to be true, ConditionsAll
	conds := []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("B", metav1.ConditionTrue),
	}

	if rule.Satisfies(&conds) {
		t.Error("expected false when only one condition matches")
	}

	// should pass
	conds = []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("C", metav1.ConditionTrue),
	}

	if !rule.Satisfies(&conds) {
		t.Error("expected true when all conditions match")
	}

	conds = []metav1.Condition{
		cond("A", metav1.ConditionTrue),
		cond("B", metav1.ConditionFalse),
		cond("C", metav1.ConditionTrue),
	}

	if !rule.Satisfies(&conds) {
		t.Error("expected true when all conditions match")
	}

	conds = []metav1.Condition{
		cond("A", metav1.ConditionFalse),
		cond("B", metav1.ConditionTrue),
		cond("C", metav1.ConditionTrue),
	}

	if !rule.Satisfies(&conds) {
		t.Error("expected true when B and C match (inner Any satisfied by B)")
	}
}

// ---- Recursed ConditionsAny(ConditionsAny(...), ConditionEquals(...)) ----

func TestRecursed_ConditionsAnyConditionsAnyAndEquals(t *testing.T) {
	// Outer Any: (inner Any with A) OR (B equals True)
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionsAny(ConditionEquals("A", metav1.ConditionTrue)),
		ConditionEquals("B", metav1.ConditionTrue),
	))

	// A true -> inner Any matches -> outer Any matches
	condsA := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if !rule.Satisfies(&condsA) {
		t.Error("expected true when inner Any matches (A true)")
	}

	// B true -> outer second branch matches
	condsB := []metav1.Condition{cond("B", metav1.ConditionTrue)}
	if !rule.Satisfies(&condsB) {
		t.Error("expected true when ConditionEquals B matches")
	}

	// Neither A nor B true -> no match
	condsNone := []metav1.Condition{
		cond("A", metav1.ConditionFalse),
		cond("B", metav1.ConditionFalse),
	}
	if rule.Satisfies(&condsNone) {
		t.Error("expected false when neither inner Any nor B matches")
	}
}

func TestRecursed_ConditionsAnyNestedAnyWithMultipleAndEquals(t *testing.T) {
	// (A or B) or C
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionsAny(
			ConditionEquals("A", metav1.ConditionTrue),
			ConditionEquals("B", metav1.ConditionTrue),
		),
		ConditionEquals("C", metav1.ConditionTrue),
	))

	// A true -> (A or B) matches
	condsA := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if !rule.Satisfies(&condsA) {
		t.Error("expected true when inner Any matches via A")
	}
	// B true -> (A or B) matches
	condsB := []metav1.Condition{cond("B", metav1.ConditionTrue)}
	if !rule.Satisfies(&condsB) {
		t.Error("expected true when inner Any matches via B")
	}
	// C true -> outer second branch matches
	condsC := []metav1.Condition{cond("C", metav1.ConditionTrue)}
	if !rule.Satisfies(&condsC) {
		t.Error("expected true when C matches")
	}
	// None of A, B, C true
	condsNone := []metav1.Condition{
		cond("A", metav1.ConditionFalse),
		cond("B", metav1.ConditionFalse),
		cond("C", metav1.ConditionFalse),
	}
	if rule.Satisfies(&condsNone) {
		t.Error("expected false when no branch matches")
	}
}

func TestRecursed_ConditionsAnyEmptyInnerAndEquals(t *testing.T) {
	// ConditionsAny() is empty (no matchers). Outer: empty Any OR ConditionEquals("A", True).
	// Empty Any never matches, so only A true can satisfy.
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionsAny(),
		ConditionEquals("A", metav1.ConditionTrue),
	))

	if !rule.Satisfies(&[]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
		t.Error("expected true when ConditionEquals A matches")
	}
	if rule.Satisfies(&[]metav1.Condition{cond("B", metav1.ConditionTrue)}) {
		t.Error("expected false when only B true (empty Any does not match)")
	}
}

// ---- PhaseUnknown constant ----

func TestPhaseUnknown(t *testing.T) {
	if PhaseUnknown != "Unknown" {
		t.Errorf("PhaseUnknown = %q, want Unknown", PhaseUnknown)
	}
}
