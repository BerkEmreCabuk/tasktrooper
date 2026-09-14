// English dictionary for the guided first-run sequence (`/setup`,
// pages/SetupPage.tsx and components/setup/*). Source of truth for SetupDict.
export const setup = {
  title: "Get TaskTrooper ready",
  description: "Four things, in order. Each one unlocks the next.",
  stepLabel: "Step {index} of {total}",
  later: "I'll do this later",
  state: {
    done: "Done",
    todo: "To do",
    unknown: "Couldn't check",
    locked: "Locked",
    desktopOnly: "Needs the Mac app",
  },
  unknownHint: "This couldn't be checked just now, so nothing here is claiming it isn't done.",
  desktopOnly: {
    title: "This step happens in the TaskTrooper Mac app",
    body: "It acts on your own Mac — checking what's installed and starting a headless Claude Code session there — and a browser tab can't reach any of that. Download the app, sign in with this same account, and this sequence carries on there.",
  },
  environment: {
    title: "Check this Mac",
    description:
      "What TaskTrooper needs on this machine: git, the Claude Code CLI and an account signed into it. Anything red below says what's wrong and what to run.",
    readyTitle: "Everything required is in place",
    readyBody: "You can connect Claude Code now.",
    notReadyTitle: "Something required is missing",
    notReadyBody: "Fix the items marked below, then press Check again. Connect stays closed until they're all green.",
    continue: "Continue",
  },
  claudeCode: {
    title: "Connect Claude Code",
    description:
      "This starts the tunnel from this Mac to TaskTrooper and installs the agent catalog into your Claude Code CLI. It takes up to a minute or two.",
    connect: "Connect Claude Code",
    connecting: "Connecting…",
    connectedTitle: "Claude Code is connected",
    connectedBody: "{binary}{version} — {agents} agents and {skills} skills installed.",
    disconnectedTitle: "Not connected yet",
    disconnectedBody: "Nothing will run until this Mac is answering for your workspace.",
    blockedByEnvironment: "The environment check has to pass first — {item} is still failing.",
    failed: "Connecting failed",
  },
  github: {
    title: "Connect GitHub",
    description:
      "Agents clone, branch, push and open pull requests as this account. You'll go to GitHub's own permission screen; nothing is typed by hand here.",
    doneTitle: "GitHub is connected",
    doneBody: "You can import repositories now.",
    todoTitle: "GitHub isn't connected yet",
    todoBody: "Without it there's nothing to import and nowhere for an agent to push.",
    continue: "Continue",
  },
  project: {
    title: "Your first project",
    description:
      "A project groups the repositories that ship together. Create one, then import your first repository into it — you'll be asked what kind it is, how it deploys, and how it's built and tested.",
    createProject: "Create a project",
    needsRepositoryTitle: "Now import a repository",
    needsRepositoryBody: "Use one of the buttons on the project below. You can import more later.",
    doneTitle: "Your first repository is in",
    doneBody: "It's linked to the project and indexing in the background. Add more whenever you like.",
    loadFailed: "Couldn't load your projects",
  },
  nav: {
    finishSetup: "Finish setup",
  },
};
