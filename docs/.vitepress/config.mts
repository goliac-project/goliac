import { defineConfig } from 'vitepress'

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: "Goliac project",
  description: "Github Organization IAC made simple",
  ignoreDeadLinks: true,
  head: [['link', { rel: 'icon', href: '/favicon.ico' }]],
  base: '/goliac',
  themeConfig: {
    outline: 'deep',
//    outline: false, // Disable/enable right sidebar globally
    logo: '/logo_small.png', 
    // https://vitepress.dev/reference/default-theme-config
    nav: [
      { text: 'Home', link: '/' },
      { text: 'Documentation',
        items: [
            { text: 'What is Goliac', link: '/what_is_goliac' },
            { text: 'Quick start', link: '/quick_start' },
            { text: 'Installation', link: '/installation' },
            { text: 'Regular Usage', link: '/regular_usage' },
            { text: 'Security', link: '/security' },
            { text: 'Troubleshooting', link: '/troubleshooting' },
            { text: 'Apis', link: '/apis' }
          ]
        }
      ],

    sidebar: [
      {
        text: 'Documentation',
        items: [
          { text: 'What is Goliac', link: '/what_is_goliac' },
          { text: 'Quick start', link: '/quick_start' },
          { text: 'Installation', link: '/installation' },
          { text: 'Regular Usage', link: '/regular_usage' },
          { text: 'Security', link: '/security' },
          { text: 'Troubleshooting', link: '/troubleshooting' },
          { text: 'Apis', link: '/apis' }
        ]
      }
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/goliac-project/goliac' }
    ],
    
    footer: {
      copyright: '<a href="https://github.com/goliac-project/goliac/blob/main/LICENSE">MIT License</a>'
    }
  }
})
