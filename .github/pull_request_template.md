## What and why

<!-- What this changes and why. Link the issue if there is one. -->

## Area

<!-- Delete the ones that don't apply. -->

- server (Go backend, agent runtime)
- desktop (Electron shell)
- desktop/ui (React UI)
- catalog (agents, prompts)

## How it was tested

<!-- Commands you ran, what you clicked through. Screenshots for UI changes. -->

## Checklist

- [ ] `make test` passes
- [ ] The title follows Conventional Commits (`feat(board): …`, `fix(desktop): …`)
- [ ] If the desktop bridge changed, `desktop/src/ipc/host.ts` and `desktop/ui/src/lib/desktop-bridge.ts` changed together
- [ ] If the server's env/stdout contract changed, `server/README.md` and `desktop/src/main/supervisor/` changed together
- [ ] New UI strings are in every locale (`npm run check:locales` in `desktop/ui`)
