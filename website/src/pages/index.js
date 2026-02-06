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
  LayoutDashboard,
  ExternalLink,
  Route,
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
          className="sc-hero__logo"
        />
        <h1 className="sc-hero__tagline">
          Task tracking for AI agents
        </h1>
        <p className="sc-hero__subtitle">
          Claude, Cursor, Codex, Gemini—same backlog. Handoffs that work. Reviews that catch bugs.
        </p>
        <div className="sc-install-command" style={{ marginBottom: '2rem' }}>
          <span>{installCmd}</span>
          <CopyButton text={installCmd} />
        </div>
        <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'center', flexWrap: 'wrap' }}>
          <Link className="sc-cta__button sc-cta__button--primary" to="/docs/intro">
            Get Started <ArrowRight size={16} />
          </Link>
          <Link className="sc-cta__button" to="/docs/core-workflow">
            Read Docs
          </Link>
          <Link className="sc-cta__button" to="https://github.com/marcus/td">
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

        <div className="sc-workflow-grid">
          {/* Header row */}
          <div className="sc-workflow-header sc-workflow-header--left">
            <Monitor size={20} />
            <span>You</span>
          </div>
          <div className="sc-workflow-header sc-workflow-header--right">
            <Terminal size={20} />
            <span>Agents</span>
          </div>

          {/* Row 1: Human creates backlog */}
          <div className="sc-workflow-cell">
            <div className="sc-workflow-item sc-workflow-item--human sc-workflow-item--arrow-to-right">
              <div className="sc-workflow-item__title">Create backlog</div>
              <div className="sc-workflow-item__desc">Define epics, break into tasks, set priorities</div>
              <code className="sc-workflow-item__code">td create "OAuth login" -p P1</code>
            </div>
          </div>
          <div className="sc-workflow-cell" />

          {/* Row 2: Agent picks up and works */}
          <div className="sc-workflow-cell" />
          <div className="sc-workflow-cell">
            <div className="sc-workflow-item sc-workflow-item--agent sc-workflow-item--arrow-down">
              <div className="sc-workflow-item__title">Pick up tasks</div>
              <div className="sc-workflow-item__desc">Start work, handle in parallel when unblocked</div>
              <code className="sc-workflow-item__code">td start td-a1b2</code>
            </div>
          </div>

          {/* Row 3: Agent handoffs */}
          <div className="sc-workflow-cell" />
          <div className="sc-workflow-cell">
            <div className="sc-workflow-item sc-workflow-item--agent sc-workflow-item--arrow-down">
              <div className="sc-workflow-item__title">Do handoffs</div>
              <div className="sc-workflow-item__desc">Record progress for next session to resume</div>
              <code className="sc-workflow-item__code">td handoff --done "..." --remaining "..."</code>
            </div>
          </div>

          {/* Row 4: Agent submits for review */}
          <div className="sc-workflow-cell" />
          <div className="sc-workflow-cell">
            <div className="sc-workflow-item sc-workflow-item--agent sc-workflow-item--arrow-to-left">
              <div className="sc-workflow-item__title">Submit for review</div>
              <div className="sc-workflow-item__desc">Different session must review—enforced isolation</div>
              <code className="sc-workflow-item__code">td review td-a1b2</code>
            </div>
          </div>

          {/* Row 5: Human reviews */}
          <div className="sc-workflow-cell">
            <div className="sc-workflow-item sc-workflow-item--human sc-workflow-item--arrow-down">
              <div className="sc-workflow-item__title">Review & approve</div>
              <div className="sc-workflow-item__desc">Verify work, request changes, or close</div>
              <code className="sc-workflow-item__code">td approve td-a1b2</code>
            </div>
          </div>
          <div className="sc-workflow-cell" />

          {/* Row 6: Human monitors */}
          <div className="sc-workflow-cell">
            <div className="sc-workflow-item sc-workflow-item--human">
              <div className="sc-workflow-item__title">Monitor progress</div>
              <div className="sc-workflow-item__desc">Watch agents work across sessions in real-time</div>
              <code className="sc-workflow-item__code">td monitor</code>
            </div>
          </div>
          <div className="sc-workflow-cell" />
        </div>
      </div>
    </section>
  );
}

function MonitorPreview() {
  return (
    <section style={{ padding: '4rem 2rem', backgroundColor: 'var(--td-bg-surface)' }}>
      <div className="sc-section-container" style={{ textAlign: 'center' }}>
        <h2 style={{ fontFamily: "'Fraunces', 'Iowan Old Style', 'Palatino', 'Times New Roman', serif", color: 'var(--td-text-primary)', fontSize: '1.75rem', marginBottom: '0.75rem' }}>
          Watch work happen in real-time
        </h2>
        <p style={{ color: 'var(--td-text-secondary)', maxWidth: 600, margin: '0 auto 2rem', fontSize: '1.05rem' }}>
          The live monitor shows current work, board swimlanes, and activity logs across all agent sessions.
        </p>
        <img
          src={useBaseUrl('/img/td-monitor.png')}
          alt="td monitor — real-time TUI dashboard"
          style={{ maxWidth: '100%', borderRadius: 8 }}
        />
      </div>
    </section>
  );
}

function AgentsSection() {
  return (
    <section className="sc-agents-section">
      <div className="sc-section-container">
        <h2 className="sc-agents-section__title">Works with your agent</h2>
        <p className="sc-agents-section__subtitle">
          Any agent that can run shell commands works with td—coding, research, data pipelines, whatever.
        </p>
        <div className="sc-agents-section__grid">
          <div className="sc-agents-section__item">
            <div className="sc-agent-logo">
              <svg viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
                <rect width="32" height="32" rx="6" fill="#D97706" />
                <path d="M16 6L8 10v12l8 4 8-4V10l-8-4zm0 2.2l5.6 2.8L16 13.8l-5.6-2.8L16 8.2zM10 11.8l5 2.5v7.4l-5-2.5v-7.4zm12 0v7.4l-5 2.5v-7.4l5-2.5z" fill="white" />
              </svg>
            </div>
            <span className="sc-agents-section__item-name">Claude Code</span>
          </div>

          <div className="sc-agents-section__item">
            <div className="sc-agent-logo">
              <svg viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
                <rect width="32" height="32" rx="6" fill="#171717" />
                <path d="M8 16a8 8 0 1 1 16 0" stroke="#F7B500" strokeWidth="2.5" strokeLinecap="round" />
                <circle cx="16" cy="16" r="3" fill="#F7B500" />
                <path d="M16 19v5" stroke="#F7B500" strokeWidth="2" strokeLinecap="round" />
              </svg>
            </div>
            <span className="sc-agents-section__item-name">Cursor</span>
          </div>

          <div className="sc-agents-section__item">
            <div className="sc-agent-logo">
              <svg viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
                <rect width="32" height="32" rx="6" fill="#10A37F" />
                <circle cx="16" cy="16" r="8" stroke="white" strokeWidth="2" fill="none" />
                <circle cx="16" cy="16" r="3" fill="white" />
              </svg>
            </div>
            <span className="sc-agents-section__item-name">Codex</span>
          </div>

          <div className="sc-agents-section__item">
            <div className="sc-agent-logo">
              <svg viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
                <rect width="32" height="32" rx="6" fill="#4285F4" />
                <path d="M16 8l-6.93 12h13.86L16 8z" fill="#EA4335" />
                <path d="M9.07 20L16 8v12H9.07z" fill="#FBBC05" />
                <path d="M22.93 20L16 8v12h6.93z" fill="#34A853" />
              </svg>
            </div>
            <span className="sc-agents-section__item-name">Gemini CLI</span>
          </div>

          <div className="sc-agents-section__item">
            <div className="sc-agent-logo">
              <svg viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
                <rect width="32" height="32" rx="6" fill="#24292e" />
                <path d="M16 8c-4.42 0-8 3.58-8 8a8.01 8.01 0 005.47 7.59c.4.07.53-.17.53-.38v-1.33c-2.22.48-2.69-1.07-2.69-1.07-.36-.92-.89-1.17-.89-1.17-.73-.5.05-.49.05-.49.8.06 1.23.82 1.23.82.71 1.22 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.65 7.65 0 014 0c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.74.54 1.49v2.21c0 .21.14.46.55.38A8.01 8.01 0 0024 16c0-4.42-3.58-8-8-8z" fill="white" />
              </svg>
            </div>
            <span className="sc-agents-section__item-name">GitHub Copilot</span>
          </div>
        </div>
      </div>
    </section>
  );
}

const pills = [
  { icon: <Terminal size={14} />, label: 'CLI-first' },
  { icon: <Database size={14} />, label: 'SQLite local' },
  { icon: <Package size={14} />, label: 'Single binary' },
  { icon: <Search size={14} />, label: 'TDQ query language' },
  { icon: <LayoutDashboard size={14} />, label: 'Configurable boards' },
  { icon: <Route size={14} />, label: 'Critical path analysis' },
  { icon: <ListChecks size={14} />, label: 'Multi-issue sessions' },
  { icon: <GitCommit size={14} />, label: 'Git snapshots' },
  { icon: <RotateCcw size={14} />, label: 'Undo support' },
  { icon: <BarChart3 size={14} />, label: 'Analytics' },
  { icon: <FileText size={14} />, label: 'File tracking' },
  { icon: <ExternalLink size={14} />, label: 'Sidecar integration', link: 'https://sidecar.haplab.com/' },
];

function FeaturesGrid() {
  return (
    <section style={{ padding: '4rem 2rem', backgroundColor: 'var(--td-bg-base)' }}>
      <div className="sc-section-container">
        <h2 style={{ textAlign: 'center', fontFamily: "'Fraunces', 'Iowan Old Style', 'Palatino', 'Times New Roman', serif", color: 'var(--td-text-primary)', fontSize: '1.75rem', marginBottom: '2rem' }}>
          Everything you need
        </h2>
        <div className="sc-features-grid" style={{ maxWidth: 900, margin: '0 auto' }}>
          {pills.map((pill, idx) => pill.link ? (
            <a href={pill.link} className="sc-features-grid__item" key={idx} target="_blank" rel="noopener noreferrer">
              <span className="sc-features-grid__item-icon">{pill.icon}</span>
              {pill.label}
            </a>
          ) : (
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

function SisterProjects() {
  return (
    <section className="sc-sister-projects">
      <div className="sc-section-container">
        <h2 className="sc-sister-projects__title">Sister Projects</h2>
        <a href="https://haplab.com" className="sc-sisterHaplab">
          <img src={useBaseUrl('/img/haplab-logo.png')} alt="Haplab" />
        </a>
        <div className="sc-sister-projects__grid">
          <a href="https://td.haplab.com/" className="sc-sister-card sc-sister-card--purple sc-sister-card--current">
            <div className="sc-sister-card__logo-wrapper">
              <img src={useBaseUrl('/img/td-logo.png')} alt="td" className="sc-sister-card__logo" />
            </div>
            <p>Task management for AI-assisted development.</p>
          </a>
          <a href="https://sidecar.haplab.com/" className="sc-sister-card sc-sister-card--green">
            <div className="sc-sister-card__logo-wrapper">
              <img src={useBaseUrl('/img/sidecar-logo.png')} alt="Sidecar" className="sc-sister-card__logo" />
            </div>
            <p>You might never open your editor again.</p>
          </a>
          <a href="https://betamax.haplab.com/" className="sc-sister-card sc-sister-card--blue">
            <div className="sc-sister-card__logo-wrapper">
              <img src={useBaseUrl('/img/betamax-logo-fuzzy.png')} alt="Betamax" className="sc-sister-card__logo" />
            </div>
            <p>Record anything you see in your terminal.</p>
          </a>
          <a href="https://nightshift.haplab.com/" className="sc-sister-card sc-sister-card--amber">
            <div className="sc-sister-card__logo-wrapper">
              <img src={useBaseUrl('/img/nightshift-logo.png')} alt="Nightshift" className="sc-sister-card__logo" />
            </div>
            <p>It finds what you forgot to look for.</p>
          </a>
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
        <MonitorPreview />
        <AgentsSection />
        <FeaturesGrid />
        <BottomCTA />
        <SisterProjects />
      </main>
    </Layout>
  );
}
