# Contributing

Thanks for considering a contribution to Notion2API.

## Development

- Keep changes focused and avoid unrelated formatting churn.
- Put backend business logic in services, database access in repositories, and
  HTTP input/output handling in handlers.
- Put frontend API calls in `frontend/src/services` and shared types in
  `frontend/src/types`.
- Do not commit credentials, local databases, logs, browser storage states, or
  generated build output.

## Checks

Run the relevant checks before submitting changes:

```bash
cd backend
go test ./...

cd ../frontend
pnpm typecheck
pnpm build
```

If a check cannot be run, mention that in the pull request.
