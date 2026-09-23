-- backend/frontend/mobile-developer each ship `subscriptions: [todo,
-- need_revision]` in the catalog, but every install that predates catalog slugs
-- adopted them by name, and the adoption path never applied the suggested
-- subscriptions. The columns stayed unowned: a task landing in todo woke nobody,
-- and the Kolonlar tab showed an empty checklist for all three.
--
-- Mirrors applySuggestedSubscriptions exactly: seed only an agent that has no
-- subscription at all, only into a column no other agent already claims, and let
-- one agent win a contested column (catalog order, i.e. agent name) so a todo
-- task does not fan out onto three developers at once.
WITH wanted(agent_name, column_slug) AS (
    VALUES ('backend-developer', 'todo'),
           ('backend-developer', 'need_revision'),
           ('frontend-developer', 'todo'),
           ('frontend-developer', 'need_revision'),
           ('mobile-developer', 'todo'),
           ('mobile-developer', 'need_revision')
),
eligible AS (
    SELECT w.column_slug,
           a.id AS agent_id,
           row_number() OVER (PARTITION BY w.column_slug ORDER BY a.name) AS rn
    FROM wanted w
    JOIN agents a ON a.name = w.agent_name
    WHERE NOT EXISTS (SELECT 1 FROM agent_column_subscriptions s WHERE s.agent_id = a.id)
      AND NOT EXISTS (SELECT 1 FROM agent_column_subscriptions s WHERE s.column_slug = w.column_slug)
      AND EXISTS (SELECT 1 FROM board_columns c WHERE c.slug = w.column_slug)
)
INSERT INTO agent_column_subscriptions (agent_id, column_slug)
SELECT agent_id, column_slug FROM eligible WHERE rn = 1
ON CONFLICT (agent_id, column_slug) DO NOTHING;
