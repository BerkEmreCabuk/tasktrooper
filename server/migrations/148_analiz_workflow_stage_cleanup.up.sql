-- Migration 143 seeded workflow_stages uniformly for all 13 board columns ×
-- all 4 task types. For analiz these 5 are vestigial copy-paste rows from
-- the task-type template: no participants were ever assigned to them, their
-- instructions are empty, and nothing in analiz's own behaviour set ever
-- routes to them. Their presence is what made every board column show up as
-- an analiz "stage" in Settings -> Workflows even though only 8 are real.
DELETE FROM workflow_stages
WHERE task_type = 'analiz'
  AND column_slug IN ('code_review', 'ready_for_qa', 'in_qa', 'pm_uat', 'human_uat');
