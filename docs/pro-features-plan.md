# Pro Features & Business Model Plan

Premium feature opportunities for td, organized by tier.

## Free (Open Source) Tier - What Exists Today

- Local-first CLI task management
- TUI monitor
- Boards, epics, dependencies, work sessions
- Self-hosted sync server
- 3 roles (owner/writer/reader)
- Device auth

## Pro Tier (Individual / Small Team)

**E2E Encryption** - Encrypt event payloads client-side so the server is zero-knowledge. Key management via project-scoped symmetric keys. Differentiator: most PM tools can read your data.

**SSO / SAML / OIDC** - Login with Okta/Google Workspace. Current device-code auth is fine for indie devs but a blocker for org adoption. Implementation via **WorkOS** - single API that handles SAML/OIDC connections to all major IdPs (Okta, Azure AD, Google Workspace). Go SDK available. Also provides Directory Sync (SCIM) which covers the enterprise provisioning feature. Free for core auth, SSO priced per-connection (pass cost through to enterprise customers).

**Audit Log & Compliance** - Immutable, exportable audit trail of who did what and when. Event sourcing already exists - this is mostly a reporting/export layer. SOC2/ISO-minded teams pay for this.

**Advanced Conflict Resolution** - Merge UI (even CLI-based) that lets users review conflicts before resolution, with optional "require manual resolution" mode per project. Current LWW is pragmatic but lossy.

**Priority Support / SLA** - Guaranteed response times for paying users.

## Team Tier

**Hosted Sync (Managed Service)** - Run the sync server so teams don't have to. Charge per-seat or per-project. Free tier: 1 project, 3 members. Paid: unlimited.

**Jira / Linear / GitHub Issues Integration** - Bidirectional sync with existing trackers. Makes td the AI-agent layer on top of existing PM tools rather than a replacement. High-value because teams can't always adopt a new tool wholesale.

**Webhooks / Notifications** - On push: notify Slack, Discord, email. Enables CI/CD integration (trigger builds when issues move to `in_review`).

**Analytics Dashboard** - Cycle time, throughput, agent efficiency metrics, rework rates. `rework()` already exists in TDQ - surface this as a web dashboard. Teams doing AI-assisted dev want to measure agent productivity.

**Role Granularity & Permissions** - Per-board or per-epic access control. Current 3-role model is coarse for larger teams.

## Enterprise Tier

**Read-Only Web View** - Web UI for managers/stakeholders who won't use the CLI. Boards, issue status, activity feed.

**Multi-Project Linking** - Cross-project dependencies, shared epics, portfolio view.

**Data Residency / Self-Hosted with License Key** - Enterprise self-hosts but pays for a license that unlocks pro features. Common model (GitLab, Mattermost, Sentry).

**SCIM Provisioning** - Auto-provision/deprovision users from IdP. Required for large org adoption alongside SSO.

**Custom Integrations API** - REST/GraphQL API for building custom workflows, dashboards, or connecting to internal tools.

## Recommended Business Model

| Approach | Pros | Cons |
|---|---|---|
| **Open-core + managed hosting** | Predictable SaaS revenue, low friction onboarding | Must run infra |
| **Open-core + license key** (GitLab model) | No infra, enterprise-friendly | Slower growth, enforcement complexity |
| **Usage-based (events/month)** | Scales with adoption | Unpredictable revenue, discourages usage |

**Recommendation**: Open-core with managed hosting as primary revenue, plus enterprise license keys for self-hosted.

### Per-User Pricing

Per-seat pricing is the most natural fit for td. Each syncing user is already a distinct entity (API key, device ID, membership role), so metering is straightforward with no new infrastructure.

| Tier | Price | Includes |
|---|---|---|
| **Free** | $0 | 1 project, 3 members, self-hosted sync only |
| **Pro** | ~$8-12/user/month | Managed hosting, unlimited projects, E2E encryption, priority support |
| **Team** | ~$15-20/user/month | Pro + integrations, webhooks, analytics dashboard, granular permissions |
| **Enterprise** | Custom | Team + SSO/SAML, SCIM, audit logs, web view, data residency, SLA |

Key considerations:
- **Count human users, not agents.** AI agents operate under a human's session/device - charging per-agent would penalize the core use case and create friction. One seat = one human, unlimited agents.
- **Free tier should be generous enough for solo devs.** The funnel is: solo dev tries td locally -> adds sync -> invites a teammate -> hits the free limit -> upgrades. The 3-member free limit is the natural conversion point.
- **Annual discount** (e.g. 2 months free) incentivizes commitment and reduces churn.
- **Billing by project owner.** The project owner's plan determines what features are available to all members. Members don't need their own paid plan to use a paid project - this simplifies adoption within teams.

### Priority order for implementation

1. **Managed sync hosting** - lowest friction to monetize, server already exists
2. **Integrations** (Jira/Linear/GitHub) - highest pull for team adoption
3. **SSO/SAML** - enterprise gate, they'll pay because they have to
4. **E2E encryption** - differentiator, "your project data stays yours"
5. **Web dashboard** - unlocks non-CLI users as stakeholders

## Positioning

The AI-agent-native angle is the moat. No other tracker is designed around session handoffs and agent workflows. Positioning: "the project tracker that AI agents actually use." Integrations tier sells itself to teams already using Cursor/Claude Code/Copilot agents.
