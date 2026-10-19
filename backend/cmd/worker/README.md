# cmd/worker

Tarefas assíncronas: poller da outbox (membros → tuplos, ADR 0015), conectores de live timing,
alertas (`pitbox-worker`).

Só contém `main.go`: lê a configuração e chama `internal/bootstrap` (`RunWorker`). Corre com
`make run-worker` contra a infraestrutura local.
