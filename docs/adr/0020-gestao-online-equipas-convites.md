# 0020 — Gestão online de equipas e convites na app

- **Estado:** Aceite (Nelson, 2026-10-28)
- **Cartão:** F1-09b (#139)
- **Relacionado:** [ADR 0003](0003-app-offline-first-sync-proprio.md), [ADR 0005](0005-openfga-rls-dupla-barreira.md), [ADR 0018](0018-base-local-web.md); arquitetura §3 e §6.

## Contexto

A app lê normalmente da base SQLite local e o modo de prova não pode depender da rede. A gestão da identidade de uma equipa é diferente: criar equipa exige fator forte; convidar, aceitar, mudar papéis e remover membros dependem da autorização e do estado atual no servidor. Aceitar um convite consome um segredo de uso único e atribui uma relação no OpenFGA.

A app F1-10 mantém localmente apenas o resumo de cada equipa e o papel do utilizador. Não tem tabelas locais de membros ou convites. Enfileirar estas operações offline permitiria apresentar um papel ou convite como válido depois de revogado, expirado ou consumido no servidor.

## Decisão

1. **Gestão de equipas e convites é online-only.** As leituras de membros/convites e as escritas passam pelos repositórios da app e pelos RPCs existentes. Não se cria uma fila offline nem se faz escrita otimista local.
2. **O servidor é sempre a autoridade.** A UI pode ocultar/desativar ações de acordo com o papel e a ligação, mas não substitui `authz.Check`, a verificação da `membership`, o RLS, o step-up nem as regras do caso de uso.
3. **A lista local de equipas é atualizada depois da confirmação do servidor.** Depois de `CreateTeam` ou `AcceptInvitation`, a app obtém a lista completa de `ListMyTeams` e atualiza a tabela `team`. Não passa uma lista parcial a `teamBootstrapStatements`, que marca equipas ausentes como apagadas.
4. **Membros, convites, emails e tokens não são persistidos na base local.** Os membros e convites pendentes vivem na cache de queries enquanto a app está aberta; os tokens de convite ficam apenas em memória até à aceitação ou à ação explícita de copiar/partilhar.
5. **Ações sem rede ficam indisponíveis e explicadas.** `navigator.onLine` serve apenas para orientação; qualquer falha real do RPC continua a ser tratada e nenhuma operação fica pendente para sincronização futura.
6. **Convites por link usam o fragmento `#t=`**, conforme o comentário do contrato protobuf. A app valida o token, remove o fragmento da entrada de histórico antes de qualquer chamada e nunca coloca o token em path, query, router state, storage, logs, traces ou telemetria. A aceitação é feita apenas por uma conta autenticada.

## Alternativas consideradas

| Alternativa | Motivo para rejeitar |
| --- | --- |
| Enfileirar criação/aceitação/gestão offline | A autorização, o fator forte, a validade do convite e o papel podem mudar enquanto o cliente está offline; não há uma forma segura de afirmar que a operação já foi aceite. |
| Guardar membros/convites na base local | Pode conservar emails/roles desatualizados e aumentar a quantidade de dados pessoais guardados no browser; não é necessário para a prova offline. |
| Fazer os componentes chamarem Connect diretamente | Viola a separação UI → repositório e dificulta validar respostas, invalidar queries e testar falhas. |
| Guardar o token de convite em `sessionStorage` para sobreviver ao login | É um segredo bearer acessível a JavaScript e viola a regra de não guardar tokens no storage web. Um utilizador sem sessão entra/regista-se e reabre o link original. |

## Consequências

- A experiência de gestão exige rede, mas garagem e dados já descarregados continuam disponíveis offline.
- A UI tem de mostrar estados de carregamento, erro, conflito de versão, falta de permissões e falta de rede.
- O refresh da tabela local usa a lista completa do servidor após criar equipa ou aceitar convite; não há nova migration SQLite nesta fase.
- O token de um convite por link é uma capacidade bearer, válida até 7 dias e de uso único. A UI avisa que qualquer pessoa com o link o pode aceitar; o backend mantém hash SHA-256, expiração, consumo atómico e erro genérico.
- O F1-09b não implementa o envio real de emails (#124), transferência de propriedade nem gestão de MFA/passkeys na app. A app encaminha o utilizador para o self-service do Zitadel e usa o step-up do BFF.
