import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Beads Documentation',
  tagline: 'Git-backed issue tracker for AI-supervised coding workflows',
  favicon: 'img/favicon.svg',

  future: {
    v4: true,
  },

  // GitHub Pages deployment
  url: 'https://joyshmitz.github.io',
  baseUrl: '/beads/',
  organizationName: 'joyshmitz',
  projectName: 'beads',
  trailingSlash: false,

  onBrokenLinks: 'warn',

  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  // Meta tags for AI agents
  headTags: [
    {
      tagName: 'meta',
      attributes: {
        name: 'llms-full',
        content: '/beads/llms-full.txt',
      },
    },
    {
      tagName: 'meta',
      attributes: {
        name: 'ai-terms',
        content: 'Load /beads/llms-full.txt for complete documentation',
      },
    },
  ],

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/', // Docs as homepage
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/joyshmitz/beads/tree/docs/docusaurus-site/website/',
          showLastUpdateTime: true,
        },
        blog: false, // Disable blog
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    // No social card image - using default
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Beads',
      logo: {
        alt: 'Beads Logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Documentation',
        },
        {
          href: 'pathname:///beads/llms.txt',
          label: 'llms.txt',
          position: 'right',
        },
        {
          href: 'https://github.com/steveyegge/beads',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Documentation',
          items: [
            {
              label: 'Getting Started',
              to: '/getting-started/installation',
            },
            {
              label: 'CLI Reference',
              to: '/cli-reference',
            },
            {
              label: 'Workflows',
              to: '/workflows/molecules',
            },
          ],
        },
        {
          title: 'Integrations',
          items: [
            {
              label: 'Claude Code',
              to: '/integrations/claude-code',
            },
            {
              label: 'MCP Server',
              to: '/integrations/mcp-server',
            },
          ],
        },
        {
          title: 'Resources',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/steveyegge/beads',
            },
            {
              label: 'llms.txt',
              href: 'pathname:///beads/llms.txt',
            },
            {
              label: 'npm Package',
              href: 'https://www.npmjs.com/package/@beads/bd',
            },
            {
              label: 'PyPI (MCP)',
              href: 'https://pypi.org/project/beads-mcp/',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Steve Yegge. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'json', 'toml', 'go'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
