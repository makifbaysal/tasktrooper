package domain

import "testing"

func TestWithoutStageRewiresPreviousStageToExit(t *testing.T) {
	w := Workflow{Stages: []WorkflowStage{
		{Column: TaskColumnInQA, OnPath: true, Position: 6, Behaviours: []BehaviourRef{{Key: BehaviourReviewVerdictSweep, Params: map[string]string{"pass_to": "pm_uat"}}}},
		{Column: TaskColumnPMUAT, OnPath: true, Position: 8, Behaviours: []BehaviourRef{
			{Key: BehaviourEnsurePROnEnter},
			{Key: BehaviourReviewVerdictSweep, Params: map[string]string{"pass_to": "human_uat"}},
			{Key: BehaviourReviewChainStage, Params: map[string]string{"label": "UAT"}},
		}},
		{Column: TaskColumnHumanUAT, OnPath: true, Position: 9},
	}}

	got := w.WithoutStage(TaskColumnPMUAT)

	if _, ok := got.Stage(TaskColumnPMUAT); ok {
		t.Fatal("pm_uat stage still present")
	}
	if to, _ := got.Param(TaskColumnInQA, BehaviourReviewVerdictSweep, "pass_to"); to != "human_uat" {
		t.Fatalf("in_qa pass_to = %q, want human_uat", to)
	}
	if !got.Has(TaskColumnHumanUAT, BehaviourEnsurePROnEnter) {
		t.Fatal("ensure_pr_on_enter not carried to human_uat")
	}
	if len(got.ReviewChain()) != 0 {
		t.Fatalf("review chain still lists UAT: %v", got.ReviewChain())
	}
	if to, _ := w.Param(TaskColumnInQA, BehaviourReviewVerdictSweep, "pass_to"); to != "pm_uat" {
		t.Fatal("receiver was mutated")
	}
}

func TestWithoutStageUnknownColumnIsNoOp(t *testing.T) {
	w := Workflow{Stages: []WorkflowStage{{Column: TaskColumnTodo}}}
	if got := w.WithoutStage(TaskColumnPMUAT); len(got.Stages) != 1 {
		t.Fatalf("stages = %d", len(got.Stages))
	}
}
