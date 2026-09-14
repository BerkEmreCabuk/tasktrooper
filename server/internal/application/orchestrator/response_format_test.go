package orchestrator_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every pipeline stage used to send domain.JSONResponseFormat() — bare
// {"type":"json_object"}, no schema — leaving a provider with strict
// structured-output support nothing to constrain decoding against. These pin
// the remaining three stages (planner has its own test in
// planner_retry_test.go) onto domain.JSONSchemaResponseFormat, using the
// scriptedLLM fixture from planner_retry_test.go — package-visible since both
// files are compiled into orchestrator_test.

func TestIntakeExtract_RequestCarriesTheJSONSchema(t *testing.T) {
	llm := &scriptedLLM{responses: []string{`{"ready":true,"purpose":"p","goal":"g","constraints":[],"questions":[]}`}}

	_, err := orchestrator.NewIntakeExtractor(llm).
		Extract(context.Background(), "siteye android linki ekle", nil, "test-model", orchestrator.IntakeOptions{Lang: "tr"})

	require.NoError(t, err)
	require.Len(t, llm.requests, 1)

	rf := llm.requests[0].ResponseFormat
	require.NotNil(t, rf, "intake must request structured output")
	assert.Equal(t, domain.ResponseFormatJSONSchema, rf.Type)
	assert.NotEmpty(t, rf.Name)
	require.NotNil(t, rf.Schema)
	props, ok := rf.Schema["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, props, "purpose")
	assert.Contains(t, props, "constraints")
}

func TestVerifierEvaluate_RequestCarriesTheJSONSchema(t *testing.T) {
	llm := &scriptedLLM{responses: []string{`{"passed":true,"issues":[],"summary":"ok"}`}}

	_, err := orchestrator.NewVerifier(llm).Evaluate(
		context.Background(), domain.GoalIntake{Purpose: "p", Goal: "g"},
		"siteye android linki ekle", map[string]string{"t1": "done"}, "test-model", "",
	)

	require.NoError(t, err)
	require.Len(t, llm.requests, 1)

	rf := llm.requests[0].ResponseFormat
	require.NotNil(t, rf, "the verifier must request structured output")
	assert.Equal(t, domain.ResponseFormatJSONSchema, rf.Type)
	assert.NotEmpty(t, rf.Name)
	require.NotNil(t, rf.Schema)
	props, ok := rf.Schema["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, props, "passed")
	assert.Contains(t, props, "issues")
}

func TestReplannerGenerate_RequestCarriesTheJSONSchema(t *testing.T) {
	agentID := uuid.New()
	accepted := `{"summary":"repair","tasks":[{"id":"r1","title":"Fix","description":"d","agent_id":"` +
		agentID.String() + `","skill_ids":[],"tool_names":["run_terminal"],"subtask_rules":[],"depends_on":[],"parallel_group":0}]}`

	llm := &scriptedLLM{responses: []string{accepted}}
	catalog := singleAgentCatalog{agent: domain.Agent{ID: agentID, Name: "backend-developer", Enabled: true}}
	replanner := orchestrator.NewReplanner(llm, catalog, 10)

	_, err := replanner.Generate(
		context.Background(),
		domain.GoalIntake{Purpose: "p", Goal: "g"},
		[]string{"missing tests"},
		domain.PlannerOutput{},
		map[string]string{"t1": "done"},
		"siteye android linki ekle", "test-model",
		orchestrator.PlannerOptions{Lang: "tr"},
	)

	require.NoError(t, err)
	require.Len(t, llm.requests, 1)

	rf := llm.requests[0].ResponseFormat
	require.NotNil(t, rf, "the replanner must request structured output")
	assert.Equal(t, domain.ResponseFormatJSONSchema, rf.Type)
	assert.NotEmpty(t, rf.Name)
	require.NotNil(t, rf.Schema)
	props, ok := rf.Schema["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, props, "tasks")
	assert.Contains(t, props, "summary")
}
