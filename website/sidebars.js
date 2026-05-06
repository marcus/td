const sidebars = {
  docsSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Features',
      items: [
        'core-workflow',
        'boards',
        'dependencies',
        'query-language',
        'epics',
        'work-sessions',
        'deferral',
        'file-tracking',
        'notes',
        'directory-associations',
      ],
    },
    {
      type: 'category',
      label: 'Tools',
      items: [
        'monitor',
        'kanban',
        'ai-integration',
        'sync-cli',
      ],
    },
    {
      type: 'category',
      label: 'HTTP API',
      items: [
        'http-api/overview',
        'http-api/api-reference',
        'http-api/authentication',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'command-reference',
        'configuration',
        'analytics',
      ],
    },
  ],
};

export default sidebars;
