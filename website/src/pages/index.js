import React, { useState } from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import {
  ArrowRight,
  GitBranch,
  Shield,
  Search,
  Network,
  Layers,
  Monitor,
  Terminal,
  Database,
  Package,
  Settings,
  GitCommit,
  RotateCcw,
  ListChecks,
  FileText,
  BarChart3,
  Check,
  Copy,
} from 'lucide-react';

function CopyButton({ text }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <button
      className="sc-install-command__copy"
      onClick={handleCopy}
      title="Copy to clipboard"
      aria-label="Copy install command"
    >
      {copied ? <Check size={16} /> : <Copy size={16} />}
    </button>
  );
}

function HeroSection() {
  const installCmd = 'go install github.com/marcus/td@latest';
  const logoUrl = useBaseUrl('/img/td-logo.png');

  return (
    <section className="sc-hero">
      <div className="sc-section-container">
        <img
          src={logoUrl}
          alt="td logo"
          style={{ maxWidth: 280, width: '100%', height: 'auto', marginBottom: '2rem' }}
        />
        <p className="sc-hero__subtitle">
          Task management for AI-assisted development
        </p>
        <p style={{ color: 'var(--td-text-secondary)', maxWidth: 640, margin: '0 auto 2rem', lineHeight: 1.7, fontSize: '1.05rem' }}>
          Structured handoffs. Session isolation. The external memory that lets
          your next AI session pick up exactly where the last one left off.
        </p>
        <div className="sc-install-command" style={{ marginBottom: '2rem' }}>
          <span>{installCmd}</span>
          <CopyButton text={installCmd} />
        </div>
        <div style={{ display: 'flex', gap: '1rem', justifyContent: 'center', flexWrap: 'wrap' }}>
          <Link className="sc-cta__button" to="/docs/intro">
            Get Started <ArrowRight size={16} />
          </Link>
          <Link
            className="sc-cta__button"
            to="/docs/core-workflow"
            style={{ background: 'var(--td-bg-elevated)', color: 'var(--td-green)', border: '1px solid var(--td-border-color)' }}
          >
            Read Docs
          </Link>
          <Link
            className="sc-cta__button"
            to="https://github.com/marcus/td"
            style={{ background: 'var(--td-bg-elevated)', color: 'var(--td-text-secondary)', border: '1px solid var(--td-border-color)' }}
          >
            GitHub
          </Link>
        </div>
      </div>
    </section>
  );
}

function TerminalMockup() {
  return (
    <div className="sc-terminal-mockup">
      <div className="sc-terminal-mockup__titlebar">
        <span className="sc-terminal-mockup__dot sc-terminal-mockup__dot--red" />
        <span className="sc-terminal-mockup__dot sc-terminal-mockup__dot--yellow" />
        <span className="sc-terminal-mockup__dot sc-terminal-mockup__dot--green" />
        <span className="sc-terminal-mockup__title">td usage</span>
      </div>
      <div className="sc-terminal-mockup__body">
        <div><span className="prompt">$ </span><span className="command">td usage</span></div>
        <div><span className="highlight">SESSION:</span> <span className="output">claude-7f3a (started 2h ago)</span></div>
        <br />
        <div><span className="warning">FOCUSED:</span> <span className="highlight">td-a1b2</span> <span className="string">"Add OAuth login"</span> <span className="output">[in_progress]</span></div>
        <div><span className="output">  Last handoff (1h ago):</span></div>
        <div><span className="output">    Done: OAuth callback, token storage</span></div>
        <div><span className="output">    Remaining: Refresh rotation, logout flow</span></div>
        <br />
        <div><span className="highlight">REVIEWABLE</span> <span className="output">(by this session):</span></div>
        <div><span className="output">  </span><span className="highlight">td-c3d4</span> <span className="string">"Fix signup validation"</span> <span className="output">[in_review]</span></div>
        <br />
        <div><span className="highlight">OPEN</span> <span className="output">(P1):</span></div>
        <div><span className="output">  </span><span className="highlight">td-e5f6</span> <span className="string">"Rate limiting on API"</span> <span className="output">[open]</span></div>
      </div>
    </div>
  );
}

const features = [
  {
    icon: <GitBranch size={28} />,
    title: 'Structured Handoffs',
    description: 'Capture done/remaining/decisions/uncertain. Next session doesn\'t guess.',
  },
  {
    icon: <Shield size={28} />,
    title: 'Session Isolation',
    description: 'Different sessions must review. Catches "works on my context" bugs.',
  },
  {
    icon: <Search size={28} />,
    title: 'Query-Based Boards',
    description: 'Organize work with TDQ queries. View as swimlanes in the monitor.',
  },
  {
    icon: <Network size={28} />,
    title: 'Dependency Graphs',
    description: 'Model dependencies. Critical-path finds optimal unblocking sequence.',
  },
  {
    icon: <Layers size={28} />,
    title: 'Epics',
    description: 'Track large initiatives spanning multiple issues with tree visualization.',
  },
  {
    icon: <Monitor size={28} />,
    title: 'Live Monitor',
    description: 'Real-time TUI dashboard watching agent activity across sessions.',
  },
];

function FeatureCards() {
  return (
    <section style={{ padding: '4rem 2rem', backgroundColor: 'var(--td-bg-surface)' }}>
      <div className="sc-section-container">
        <h2 style={{ textAlign: 'center', fontFamily: "'JetBrains Mono', monospace", color: 'var(--td-text-primary)', fontSize: '1.75rem', marginBottom: '3rem' }}>
          Built for AI-agent workflows
        </h2>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: '1.5rem' }}>
          {features.map((feature, idx) => (
            <div className="sc-feature-card" key={idx}>
              <span className="sc-feature-card__icon">{feature.icon}</span>
              <h3 className="sc-feature-card__title">{feature.title}</h3>
              <p className="sc-feature-card__description">{feature.description}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function WorkflowSection() {
  return (
    <section className="sc-workflow-section">
      <div className="sc-section-container">
        <h2 className="sc-workflow-section__title">How it works</h2>
        <p style={{ textAlign: 'center', color: 'var(--td-text-secondary)', maxWidth: 700, margin: '0 auto 3rem', fontSize: '1.05rem' }}>
          You build the backlog. Agents work through it autonomously—in parallel, with handoffs and enforced review.
        </p>

        <div className="sc-workflow-columns">
          <div className="sc-workflow-column">
            <div className="sc-workflow-column__header">
              <span className="sc-workflow-column__icon">👤</span>
              <span className="sc-workflow-column__title">You</span>
            </div>

            <div className="sc-workflow-item">
              <div className="sc-workflow-item__title">Create backlog</div>
              <div className="sc-workflow-item__desc">Define epics, break into tasks, set priorities</div>
              <code className="sc-workflow-item__code">td create "OAuth login" -p P1</code>
            </div>

            <div className="sc-workflow-arrow">→</div>

            <div className="sc-workflow-spacer" />

            <div className="sc-workflow-arrow">←</div>

            <div className="sc-workflow-item">
              <div className="sc-workflow-item__title">Review & approve</div>
              <div className="sc-workflow-item__desc">Verify work, request changes, or approve</div>
              <code className="sc-workflow-item__code">td approve td-a1b2</code>
            </div>

            <div className="sc-workflow-spacer" />

            <div className="sc-workflow-item">
              <div className="sc-workflow-item__title">Monitor in real-time</div>
              <div className="sc-workflow-item__desc">Watch agents work across sessions</div>
              <code className="sc-workflow-item__code">td monitor</code>
            </div>
          </div>

          <div className="sc-workflow-divider" />

          <div className="sc-workflow-column">
            <div className="sc-workflow-column__header">
              <span className="sc-workflow-column__icon">🤖</span>
              <span className="sc-workflow-column__title">Agents</span>
            </div>

            <div className="sc-workflow-spacer" />

            <div className="sc-workflow-arrow">←</div>

            <div className="sc-workflow-item">
              <div className="sc-workflow-item__title">Pick up tasks</div>
              <div className="sc-workflow-item__desc">Start work, handle in parallel if ready</div>
              <code className="sc-workflow-item__code">td start td-a1b2</code>
            </div>

            <div className="sc-workflow-item sc-workflow-item--indent">
              <div className="sc-workflow-item__title">Do handoffs</div>
              <div className="sc-workflow-item__desc">Record progress for next session</div>
              <code className="sc-workflow-item__code">td handoff --done "..." --remaining "..."</code>
            </div>

            <div className="sc-workflow-item sc-workflow-item--indent">
              <div className="sc-workflow-item__title">Submit for review</div>
              <div className="sc-workflow-item__desc">Different agent/session must review</div>
              <code className="sc-workflow-item__code">td review td-a1b2</code>
            </div>

            <div className="sc-workflow-arrow">→</div>
          </div>
        </div>
      </div>
    </section>
  );
}

const agents = [
  'Claude Code',
  'Cursor',
  'Codex',
  'GitHub Copilot',
  'Gemini CLI',
];

function AgentsSection() {
  return (
    <section className="sc-agents-section">
      <div className="sc-section-container">
        <h2 className="sc-agents-section__title">Works with your agent</h2>
        <p className="sc-agents-section__subtitle">
          Any AI coding agent that can run shell commands works with td.
        </p>
        <div className="sc-agents-section__grid">
          {agents.map((agent, idx) => (
            <div className="sc-agents-section__item" key={idx}>
              <Terminal size={24} className="sc-agents-section__item-icon" />
              <span className="sc-agents-section__item-name">{agent}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

const pills = [
  { icon: <Terminal size={14} />, label: 'CLI-first' },
  { icon: <Database size={14} />, label: 'SQLite local' },
  { icon: <Package size={14} />, label: 'Single binary' },
  { icon: <Settings size={14} />, label: 'Zero config' },
  { icon: <Search size={14} />, label: 'Query language' },
  { icon: <GitCommit size={14} />, label: 'Git integration' },
  { icon: <RotateCcw size={14} />, label: 'Undo support' },
  { icon: <ListChecks size={14} />, label: 'Multi-issue sessions' },
  { icon: <FileText size={14} />, label: 'File tracking' },
  { icon: <BarChart3 size={14} />, label: 'Analytics' },
];

function FeaturesGrid() {
  return (
    <section style={{ padding: '4rem 2rem', backgroundColor: 'var(--td-bg-base)' }}>
      <div className="sc-section-container">
        <h2 style={{ textAlign: 'center', fontFamily: "'Fraunces', 'Iowan Old Style', 'Palatino', 'Times New Roman', serif", color: 'var(--td-text-primary)', fontSize: '1.75rem', marginBottom: '2rem' }}>
          Everything you need
        </h2>
        <div className="sc-features-grid" style={{ maxWidth: 900, margin: '0 auto' }}>
          {pills.map((pill, idx) => (
            <div className="sc-features-grid__item" key={idx}>
              <span className="sc-features-grid__item-icon">{pill.icon}</span>
              {pill.label}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function BottomCTA() {
  const installCmd = 'go install github.com/marcus/td@latest';

  return (
    <section className="sc-cta">
      <div className="sc-section-container">
        <h2 className="sc-cta__title">Get started in seconds</h2>
        <div className="sc-install-command" style={{ marginBottom: '2rem' }}>
          <span>{installCmd}</span>
          <CopyButton text={installCmd} />
        </div>
        <div>
          <Link className="sc-cta__button" to="/docs/intro">
            Read the docs <ArrowRight size={16} />
          </Link>
        </div>
      </div>
    </section>
  );
}

export default function Home() {
  return (
    <Layout title="td" description="Task management for AI-assisted development">
      <main>
        <HeroSection />
        <TerminalMockup />
        <FeatureCards />
        <WorkflowSection />
        <AgentsSection />
        <FeaturesGrid />
        <BottomCTA />
      </main>
    </Layout>
  );
}
