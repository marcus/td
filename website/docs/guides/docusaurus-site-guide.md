---
sidebar_label: "Maintaining the Docs Site"
---

# Maintaining the Docs Site

A practical guide for working with the td Docusaurus documentation site.

## Quick Start

```bash
cd website
npm install
npm start    # Dev server at localhost:3000
```

## Project Structure

```
website/
├── docs/           # Documentation markdown files
├── src/
│   ├── pages/      # Custom pages (index.js = landing page)
│   └── css/        # Custom stylesheets (custom.css)
├── static/
│   ├── img/        # Images (td-logo.png, screenshots)
│   └── .nojekyll   # Prevents Jekyll processing on GitHub Pages
├── docusaurus.config.js   # Site configuration
├── sidebars.js            # Documentation sidebar structure
└── package.json           # Dependencies
```

## Writing Documentation

- Files go in `website/docs/`
- Frontmatter: `sidebar_position` controls order in the sidebar
- Use standard Markdown plus Docusaurus extensions (admonitions, tabs)
- Add new docs to `sidebars.js` when creating them

Example frontmatter:

```markdown
---
sidebar_position: 3
sidebar_label: "My New Page"
---

# My New Page

Content here.
```

## Editing the Front Page

The landing page lives at `src/pages/index.js`. Key components:

- **HeroSection** - Main headline and CTA
- **TerminalMockup** - Animated terminal demo
- **FeatureCards** - Feature highlights grid
- **WorkflowSection** - Workflow explanation
- **AgentsSection** - AI agent integration info
- **FeaturesGrid** - Detailed features list
- **BottomCTA** - Final call to action

Notes:
- Uses `sc-*` CSS classes defined in `custom.css`
- Uses `lucide-react` for icons (no emoji)

## Site Configuration

`docusaurus.config.js` controls:

- **title, tagline, URL** - Basic site metadata
- **Navbar and footer** - Navigation links
- **Color mode** - Set to dark only
- **Google Fonts** - JetBrains Mono, Inter, and Fraunces loaded via `headTags`

## Adding Images

- Place images in `website/static/img/`
- Reference them as `/img/filename.png` in docs and pages
- Site logo: `static/img/td-logo.png`

## Building and Deploying

```bash
npm run build          # Build static site to build/
npm run serve          # Preview the built site locally
```

Deployment:
- Automatic via GitHub Actions on push to `main`
- Only triggers when files in `website/**` change

## Style Guidelines

- **Never use emoji** - use Lucide icons or custom SVGs instead. Emoji are prohibited across the entire site (landing page, docs, components). Use `lucide-react` for UI icons and inline SVGs for brand/agent logos.
- **Soft dark theme** with pastel purple (`#d8b4fe`) accents and pastel blue (`#89d4ff`) CTAs
- **Fonts**: JetBrains Mono for code/nav, Fraunces serif for section headers, Inter for body text
- **CSS class prefix**: `sc-*` for all custom component styles
- **Agent icons**: Custom inline SVGs (sidecar-style) rather than icon library components

## Common Tasks

**Adding a new doc page:**
1. Create a `.md` file in `docs/`
2. Add frontmatter with `sidebar_position`
3. Add the entry to `sidebars.js`

**Changing theme colors:**
- Edit CSS variables in `src/css/custom.css`

**Adding a feature card:**
- Add to the features array in `src/pages/index.js`

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Broken links | Check `sidebars.js` matches actual doc file names |
| Build fails | Run `npm run clear` then rebuild |
| Fonts not loading | Check `headTags` in `docusaurus.config.js` |
| Stale cache | Delete `.docusaurus/` directory and restart |
