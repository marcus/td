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
        'file-tracking',
      ],
    },
    {
      type: 'category',
      label: 'Tools',
      items: [
        'monitor',
        'ai-integration',
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
