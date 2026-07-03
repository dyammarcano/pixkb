# BACEN Pix Concepts (chaves, QR, segurança, notificação)

Canonical BACEN definitions for core Pix arrangement concepts that are queried
directly but were thin in the KB. Specification-level only — what BACEN defines,
independent of any participant's implementation.

## Tipos de Chave Pix (CPF, CNPJ, e-mail, telefone, EVP)

A **chave Pix** (Pix key / alias) is the identifier a payer uses to address a
transfer, resolved through the **DICT** (Diretório de Identificadores de Contas
Transacionais) to the underlying account. BACEN defines exactly **five tipos de
chave**:

| Tipo de chave | Formato | Observação |
|---|---|---|
| **CPF** | 11 dígitos | pessoa física; uma única chave CPF por titular |
| **CNPJ** | 14 dígitos | pessoa jurídica; uma única chave CNPJ por titular |
| **E-mail** | endereço de e-mail válido | |
| **Telefone celular** | número no formato E.164 (+55…) | |
| **Chave aleatória (EVP)** | UUID v4 (32 hex + hífens) | gerada pelo PSP; *Endereçamento Virtual de Pagamento* |

**Termos:** tipos de chave pix, chave aleatória, EVP, chave CPF, chave CNPJ,
chave e-mail, chave telefone, alias Pix, DICT.

**Limites (BACEN-normative):** conta de **pessoa física** pode ter até **5
chaves**; conta de **pessoa jurídica** até **20 chaves**. CPF/CNPJ podem ser
vinculados a uma única conta; e-mail/telefone/EVP podem ser portados entre
contas (portabilidade) e reivindicados (reivindicação de posse) via DICT.

## QR Code Estático Pix (BR Code / EMV MPM)

O **QR Code estático** é um **BR Code** (padrão **EMV MPM** — Merchant Presented
Mode) que codifica os dados de recebimento de forma **reutilizável**: é gerado
**uma única vez** e pode ser pago **múltiplas vezes**, sem ida ao PSP a cada
pagamento.

- **Valor:** opcional — pode ter **valor fixo** ou ser **sem valor** (o pagador
  informa o montante).
- **txid:** opcional no estático (diferente do dinâmico, onde é obrigatório).
- **Uso típico:** recebedores de baixo volume, pessoa física, cobranças simples.

**Termos:** qr code estático pix, BR Code, EMV MPM, QR estático, cobrança Pix
estática, QR reutilizável.

**Contraste — QR dinâmico:** gerado por cobrança, payload aponta por **URL** a um
documento no PSP (location), **txid obrigatório**, suporta vencimento, juros,
multa e abatimento (Pix Cobrança / cob e cobv). O estático não carrega esses
campos.

## Requisitos de Segurança Pix (mTLS e certificados)

A comunicação nas APIs do Pix (API Pix e API DICT) e na rede do SPI exige:

- **mTLS (mutual TLS):** autenticação mútua cliente-servidor por **certificados
  ICP-Brasil**; ambas as pontas apresentam e validam certificado.
- **OAuth 2.0** (fluxo *client_credentials*): obtenção de token de acesso para as
  chamadas de API, com escopos por operação.
- **RSFN/SPI:** tráfego entre PSPs e o SPI trafega pela Rede do Sistema
  Financeiro Nacional, com requisitos de rede e assinatura de mensagens.

**Termos:** requisitos de segurança Pix, mTLS, mutual TLS, certificado
ICP-Brasil, OAuth2, autenticação mútua, segurança da API Pix, assinatura de
mensagem.

**BACEN-normative:** o mTLS protege o canal; a autorização por OAuth2 protege a
operação. A ausência de certificado válido ou de token com escopo correto
resulta em recusa antes do processamento.

## Reentrega de Notificação (Webhook Pix)

O **PSP recebedor** notifica o usuário recebedor de um **Pix recebido** por meio
de um **webhook** registrado (endpoint HTTPS do recebedor). Quando a entrega
falha, o PSP executa **reentrega (retry)** da notificação.

- **Reentrega:** em caso de falha (timeout, erro 5xx, indisponibilidade), o PSP
  **retransmite** a notificação segundo política de **retentativa com espera
  progressiva (backoff)**.
- **Idempotência:** o recebedor deve tratar a notificação de forma **idempotente**
  — uma mesma liquidação pode ser notificada mais de uma vez.
- **Reconciliação:** o recebedor pode **consultar** os Pix recebidos (endpoint de
  consulta) para suprir notificações eventualmente não entregues.

**Termos:** reentrega de notificação webhook, retentativa de webhook,
redelivery, notificação Pix recebido, webhook idempotente, backoff de
notificação, reenvio de webhook.
