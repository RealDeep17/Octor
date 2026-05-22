/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./templates/**/*.html', './assets/src/**/*.{js,jsx}'],
  theme: {
    fontFamily: {
      'sans': ['Inter', 'system-ui', 'sans-serif'],
      'logo': ['Comfortaa', 'cursive'],
    },
    extend: {
      minWidth: {
        '80': '20rem',
      },
      zIndex: {
        'dropdown':       '10',
        'sticky':         '20',
        'navbar':         '30',
        'modal-backdrop': '40',
        'modal':          '50',
        'toast':          '9999',
        'tooltip':        '70',
      },
      colors: {
        w: {
          bg:      'var(--o-bg)',
          surface: 'var(--o-surface)',
          card:    'var(--o-card)',
          pink:    'var(--o-primary)',
          pinkL:   'var(--o-primary-l)',
          primary: 'var(--o-primary)',
          primaryL:'var(--o-primary-l)',
          purple:  'var(--o-purple)',
          purpleL: 'var(--o-purple-l)',
          cyan:    'var(--o-cyan)',
          text:    'var(--o-text)',
          sub:     'var(--o-sub)',
          muted:   'var(--o-muted)',
          line:    'var(--o-line)',
        },
        o: {
          bg:      'var(--o-bg)',
          surface: 'var(--o-surface)',
          card:    'var(--o-card)',
          primary: 'var(--o-primary)',
          primaryL:'var(--o-primary-l)',
          purple:  'var(--o-purple)',
          purpleL: 'var(--o-purple-l)',
          cyan:    'var(--o-cyan)',
          text:    'var(--o-text)',
          sub:     'var(--o-sub)',
          muted:   'var(--o-muted)',
          line:    'var(--o-line)',
        }
      },
    },
  },
}
