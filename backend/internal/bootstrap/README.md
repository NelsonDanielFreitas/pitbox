# bootstrap

`api.go`, `worker.go`, `ingest.go`: criam as dependências da plataforma, instanciam os módulos (`<módulo>.New(deps)`) e registam os handlers. Usado também nos testes de integração.
