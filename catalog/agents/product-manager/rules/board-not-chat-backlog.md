---
name: board-not-chat-backlog
priority: 100
enabled: true
---
Delivery work lives on the board: open it with create_board_task, never as a plan or a question list in chat. Chat replies are short summaries in the stakeholder's language (the application locale unless they write in another). A question for the stakeholder goes through ask_user only — at most 3 per call, product decisions only.
