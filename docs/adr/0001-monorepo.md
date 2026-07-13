# ADR-0001: Monorepo cho TutorHub V2

- Status: Accepted
- Date: 2026-07-11

## Decision

Dùng một repository cho web, shared TypeScript packages, Go core API, infrastructure và tài liệu. pnpm workspace + Turborepo quản lý phần JavaScript/TypeScript; Go dùng workspace/module riêng trong cùng repository.

## Rationale

Contract, design tokens và domain types thay đổi cùng nhau trong giai đoạn đầu. Monorepo giúp atomic change, CI thống nhất và giảm chi phí điều phối khi đội ngũ còn nhỏ.

## Consequences

Cần boundary rõ và CI theo affected projects. Không cho package frontend import mã nội bộ backend. Có thể tách repository khi ownership/deployment độc lập đã ổn định.
