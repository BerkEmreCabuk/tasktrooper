-- The git, github and gitlab templates were seeded from npm packages that are
-- now deprecated or were never published, so an enabled row could not connect.
-- Only rows still carrying the seeded defaults are repointed; anything the user
-- edited is left alone.

UPDATE mcp_servers
SET command = 'uvx',
    args = '["mcp-server-git", "--repository", "."]'::jsonb
WHERE id = 'git'
  AND command = 'npx'
  AND args @> '["@modelcontextprotocol/server-git"]'::jsonb;

UPDATE mcp_servers
SET transport = 'http',
    command = '',
    args = '[]'::jsonb,
    url = 'https://api.githubcopilot.com/mcp/',
    env = '{}'::jsonb
WHERE id = 'github'
  AND transport = 'stdio'
  AND args @> '["@modelcontextprotocol/server-github"]'::jsonb;

-- The stored token was a bare PAT under env; the hosted server wants a full
-- Authorization header value, which no SQL can build out of the ciphertext.
DELETE FROM mcp_server_secrets
WHERE server_id = 'github'
  AND location = 'env'
  AND key = 'GITHUB_PERSONAL_ACCESS_TOKEN';

INSERT INTO mcp_servers (id, enabled, transport, command, args, env, url, headers, allowed_tools)
SELECT 'gitlab', false, 'stdio', 'npx',
       '["-y", "@zereight/mcp-gitlab"]'::jsonb,
       '{"GITLAB_API_URL": "https://gitlab.com/api/v4"}'::jsonb,
       '', '{}'::jsonb, '[]'::jsonb
WHERE EXISTS (SELECT 1 FROM mcp_servers)
  AND NOT EXISTS (SELECT 1 FROM mcp_servers WHERE id = 'gitlab');
