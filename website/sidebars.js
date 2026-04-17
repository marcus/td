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
        'notes',
        'epics',
        'work-sessions',
        'deferral',
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
      ],
    },
  ],
};

export default sidebars;
