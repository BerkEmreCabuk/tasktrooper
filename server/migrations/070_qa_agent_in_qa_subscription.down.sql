-- Reverse 070: drop the in_qa subscription again, leaving qa-agent on
-- ready_for_qa alone. Only removed for an agent that still holds ready_for_qa,
-- so an admin who deliberately moved QA to in_qa only keeps their setup.
DELETE FROM agent_column_subscriptions s
USING agents a
WHERE a.id = s.agent_id
  AND a.name = 'qa-agent'
  AND s.column_slug = 'in_qa'
  AND EXISTS (
      SELECT 1 FROM agent_column_subscriptions r
      WHERE r.agent_id = s.agent_id AND r.column_slug = 'ready_for_qa'
  );
