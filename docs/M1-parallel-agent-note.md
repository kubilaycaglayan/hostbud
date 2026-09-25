# M1 Parallel Work Handoff

The safest M1 task to implement in parallel is **T8A — PostgreSQL persistence and operator access**.

The current checkout indicates that the main-flow agent is working on **T9 (`internal/tmux/`)** and has an uncommitted `Makefile` change. T8A is otherwise independent of the tmux package, but the shared checkout requires clear file ownership.

## Proposed ownership

T8A agent owns:

- `internal/store/**`
- PostgreSQL migrations and repository tests
- database-related Compose configuration and volumes
- `.env.example` database variables
- T8A-related README, architecture, and acceptance/task coverage updates

The T9/main-flow agent should avoid those files while T8A is in progress.

The `Makefile` is currently modified by the T9 agent. Do not edit or overwrite that file without coordinating first; integrate any required T8A commands carefully after the existing change is committed or explicitly released.

## Do not start yet

- **T8B** depends on T8A and changes the authentication/store/API/E2E surface.
- **T10–T13** depend on the in-progress tmux and service contracts.
- **T14–T17** depend on the backend API/events contracts and overlap the frontend flow.
- **T18** is a milestone-wide documentation and acceptance audit.

## T8A completion requirements

Keep the work limited to PostgreSQL persistence and operator access. Include the assigned unit and integration tests, update the coverage lines and E2E note in the acceptance/task docs, and verify that the disposable E2E stack can boot against PostgreSQL without touching the production volume or credentials.

Before handing back, run the relevant Dockerized checks, including `make lint test`, `make e2e`, and `make gitleaks`, without changing or stopping the other agent’s work.
