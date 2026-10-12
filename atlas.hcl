# Atlas Community (ADR 0010): só versiona, valida e aplica migrações SQL escritas à mão.
# O `migrate diff` e o `migrate lint` são Pro e não se usam; ver backend/db/README.md.
# Corre sempre a partir da raiz do repositório (o Makefile garante-o).
env "local" {
  url = getenv("DATABASE_URL") # ligação como pitbox_migrator; vem do ambiente, nunca do ficheiro
  migration {
    dir = "file://backend/db/migrations"
  }
}
