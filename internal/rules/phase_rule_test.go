//go:build unit

// thanks cursor

package rules

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
	if rule.Satisfies([]metav1.Condition{}) {
		t.Error("expected false when conditions is empty")
	}
}

func TestPhaseRuleAll_Satisfies_OneCondition_Matching(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if !rule.Satisfies(conds) {
		t.Error("expected true when single required condition matches")
	}
}

func TestPhaseRuleAll_Satisfies_OneCondition_WrongStatus(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionFalse)}
	if rule.Satisfies(conds) {
		t.Error("expected false when status does not match")
	}
	conds[0].Status = metav1.ConditionUnknown
	if rule.Satisfies(conds) {
		t.Error("expected false when status is Unknown")
	}
}

func TestPhaseRuleAll_Satisfies_OneCondition_Missing(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("B", metav1.ConditionTrue)}
	if rule.Satisfies(conds) {
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
	if !rule.Satisfies(conds) {
		t.Error("expected true when all required conditions match")
	}
}

func TestPhaseRuleAll_Satisfies_MultipleConditions_OneMissing(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
		ConditionEquals("B", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if rule.Satisfies(conds) {
		t.Error("expected false when one required condition is missing")
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
	if rule.Satisfies(conds) {
		t.Error("expected false when one condition has wrong status")
	}
}

func TestPhaseRuleAll_Satisfies_AllowedStatuses_OneMatches(t *testing.T) {
	// Same condition type, multiple allowed statuses (True or Unknown)
	rule := NewPhaseRule("Pending", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue, metav1.ConditionUnknown),
	))
	if !rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
		t.Error("expected true when condition is True")
	}
	if !rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionUnknown)}) {
		t.Error("expected true when condition is Unknown")
	}
	if rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionFalse)}) {
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
	if !rule.Satisfies(conds) {
		t.Error("expected true; extra conditions should not affect match")
	}
}

func TestPhaseRuleAll_Satisfies_EmptyRule(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll())
	if !rule.Satisfies(nil) {
		t.Error("empty All rule should satisfy any conditions (no requirements)")
	}
	if !rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
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
	if got := rule.ComputePhase(conds); got != "Ready" {
		t.Errorf("ComputePhase() = %q, want %q", got, "Ready")
	}
}

func TestPhaseRuleAll_ComputePhase_NotSatisfied(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAll(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionFalse)}
	if got := rule.ComputePhase(conds); got != PhaseUnknown {
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
	if rule.Satisfies([]metav1.Condition{}) {
		t.Error("expected false when conditions is empty")
	}
}

func TestPhaseRuleAny_Satisfies_OneCondition_Matching(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionTrue)}
	if !rule.Satisfies(conds) {
		t.Error("expected true when single condition matches")
	}
}

func TestPhaseRuleAny_Satisfies_OneCondition_WrongStatus(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionFalse)}
	if rule.Satisfies(conds) {
		t.Error("expected false when status does not match")
	}
}

func TestPhaseRuleAny_Satisfies_OneCondition_Missing(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("B", metav1.ConditionTrue)}
	if rule.Satisfies(conds) {
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
	if !rule.Satisfies(conds) {
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
	if rule.Satisfies(conds) {
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
	if !rule.Satisfies(conds) {
		t.Error("expected true when all conditions match (any still satisfies)")
	}
}

func TestPhaseRuleAny_Satisfies_AllowedStatuses_OneMatches(t *testing.T) {
	rule := NewPhaseRule("Pending", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue, metav1.ConditionUnknown),
	))
	if !rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
		t.Error("expected true when condition is True")
	}
	if !rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionUnknown)}) {
		t.Error("expected true when condition is Unknown")
	}
	if rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionFalse)}) {
		t.Error("expected false when condition is False")
	}
}

func TestPhaseRuleAny_Satisfies_EmptyRule(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny())
	if rule.Satisfies(nil) {
		t.Error("empty Any rule with nil conditions should not satisfy")
	}
	if rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
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
	if got := rule.ComputePhase(conds); got != "Ready" {
		t.Errorf("ComputePhase() = %q, want %q", got, "Ready")
	}
}

func TestPhaseRuleAny_ComputePhase_NotSatisfied(t *testing.T) {
	rule := NewPhaseRule("Ready", ConditionsAny(
		ConditionEquals("A", metav1.ConditionTrue),
	))
	conds := []metav1.Condition{cond("A", metav1.ConditionFalse)}
	if got := rule.ComputePhase(conds); got != PhaseUnknown {
		t.Errorf("ComputePhase() = %q, want %q", got, PhaseUnknown)
	}
}

// ---- ConditionEquals multiple statuses (regression for closure capture) ----

func TestConditionEquals_MultipleStatuses_AllReturnedCorrectly(t *testing.T) {
	matchers := ConditionEquals("X", metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown)
	if len(matchers) != 3 {
		t.Fatalf("expected 3 matchers, got %d", len(matchers))
	}
	seen := make(map[metav1.ConditionStatus]bool)
	for _, m := range matchers {
		condType, status := m()
		if condType != "X" {
			t.Errorf("matcher returned condition type %q, want X", condType)
		}
		seen[status] = true
	}
	for _, s := range []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown} {
		if !seen[s] {
			t.Errorf("expected to see status %q among matchers", s)
		}
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
	if !rule.Satisfies(conds) {
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
	if !rule.Satisfies([]metav1.Condition{cond("A", metav1.ConditionTrue)}) {
		t.Error("expected true when first group matches")
	}
	// Only B matches
	if !rule.Satisfies([]metav1.Condition{cond("B", metav1.ConditionTrue)}) {
		t.Error("expected true when second group matches")
	}
	// Neither matches
	if rule.Satisfies([]metav1.Condition{
		cond("A", metav1.ConditionFalse),
		cond("B", metav1.ConditionFalse),
	}) {
		t.Error("expected false when no group matches")
	}
}

// ---- PhaseUnknown constant ----

func TestPhaseUnknown(t *testing.T) {
	if PhaseUnknown != "Unknown" {
		t.Errorf("PhaseUnknown = %q, want Unknown", PhaseUnknown)
	}
}
