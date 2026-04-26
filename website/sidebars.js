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
        'notes',
        'file-tracking',
      ],
    },
    {
      type: 'category',
      label: 'Tools',
      items: [
        'monitor',
        'kanban',
        'ai-integration',
      ],
    },
    {
      type: 'category',
      label: 'Collaboration',
      items: [
        'sync',
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
        'analytics',
        'configuration',
        'troubleshooting',
      ],
    },
  ],
};

export default sidebars;
