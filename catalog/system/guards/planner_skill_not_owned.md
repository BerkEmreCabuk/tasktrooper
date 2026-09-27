---
key: guard.planner_skill_not_owned
version: "1"
inputs: [TaskID, SkillID, AgentID]
---
task {{.TaskID}} skill_id {{.SkillID}} does not belong to agent {{.AgentID}}
