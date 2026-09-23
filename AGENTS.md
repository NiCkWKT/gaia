# Agent Development Guide

## Tech Stack

- Go 1.26.5
- [Chi](https://github.com/go-chi/chi) for HTTP Router
- DONT use any ORM for interacting with relational database. You should use [sqlc](https://sqlc.dev/) instead

## Principles

Write clean, professional, maintainable code.

The overriding goal is simplicity: fewer, smaller files and fewer functions. Avoid speculative generality.

## Commands

- Run tests: `go test ./...`
- Format Go code using `.golangci.yaml`: `golangci-lint fmt`
- Run configured linters: `golangci-lint run`

## Comments

- English only: comments, docstrings, log strings, CLI help. (User-facing translatable strings go through i18n — not this rule's concern.) If the repo is non-English, match it.
- Comments sparse, one-line, only for the genuinely non-obvious. Restating code is noise.
- Comments must be self-contained and explain why, not how or what. Don't narrate what a code block does or how it does it — the code already shows that. But only document the "why" when specific codes are hard to understand without the context of the comments. Avoid verbose "why" rationale in comments.

## Agent skills

### Issue tracker

Issues and specs live as GitHub issues, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical triage roles, each label string equal to its role name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
