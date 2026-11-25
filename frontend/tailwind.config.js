/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{js,jsx,ts,tsx}'],
  theme: {
    extend: {
      colors: {
        'primary-fixed': '#d8e2ff',
        'outline-variant': '#424754',
        'on-secondary-fixed': '#002113',
        'surface-variant': '#2d3449',
        'surface-container-highest': '#2d3449',
        'on-surface': '#dae2fd',
        'on-secondary-fixed-variant': '#005236',
        'on-error-container': '#ffdad6',
        'inverse-surface': '#dae2fd',
        'error-container': '#93000a',
        'outline': '#8c909f',
        'surface-container': '#171f33',
        'surface-container-low': '#131b2e',
        'on-background': '#dae2fd',
        'background': '#0b1326',
        'surface-bright': '#31394d',
        'tertiary-fixed-dim': '#ffb95f',
        'surface-container-lowest': '#060e20',
        'on-secondary': '#003824',
        'on-primary': '#002e6a',
        'on-primary-container': '#00285d',
        'on-surface-variant': '#c2c6d6',
        'primary-fixed-dim': '#adc6ff',
        'on-tertiary-fixed': '#2a1700',
        'inverse-on-surface': '#283044',
        'secondary-fixed-dim': '#4edea3',
        'secondary-container': '#00a572',
        'on-primary-fixed': '#001a42',
        'primary-container': '#4d8eff',
        'tertiary': '#ffb95f',
        'surface': '#0b1326',
        'on-tertiary-container': '#3e2400',
        'on-error': '#690005',
        'surface-container-high': '#222a3d',
        'surface-dim': '#0b1326',
        'primary': '#3b82f6',
        'on-tertiary': '#472a00',
        'tertiary-container': '#ca8100',
        'on-primary-fixed-variant': '#004395',
        'on-secondary-container': '#00311f',
        'secondary': '#4edea3',
        'secondary-fixed': '#6ffbbe',
        'surface-tint': '#adc6ff',
        'on-tertiary-fixed-variant': '#653e00',
        'error': '#ffb4ab',
        'tertiary-fixed': '#ffddb8',
        'inverse-primary': '#005ac2',
        'info': '#38bdf8'
      },
      borderRadius: {
        DEFAULT: '0.25rem',
        lg: '0.5rem',
        xl: '0.75rem',
        full: '9999px'
      },
      spacing: {
        'mobile-margin': '16px',
        unit: '8px',
        'bento-gap': '24px',
        gutter: '16px',
        'container-padding': '32px'
      },
      fontFamily: {
        'label-md': ['Plus Jakarta Sans', 'Inter', 'sans-serif'],
        'body-sm': ['Plus Jakarta Sans', 'Inter', 'sans-serif'],
        'headline-lg': ['Plus Jakarta Sans', 'Inter', 'sans-serif'],
        'headline-lg-mobile': ['Plus Jakarta Sans', 'Inter', 'sans-serif'],
        'display-lg': ['Plus Jakarta Sans', 'Inter', 'sans-serif'],
        'body-lg': ['Plus Jakarta Sans', 'Inter', 'sans-serif'],
        'headline-md': ['Plus Jakarta Sans', 'Inter', 'sans-serif'],
        'body-md': ['Plus Jakarta Sans', 'Inter', 'sans-serif']
      },
      fontSize: {
        'label-md': ['12px', { lineHeight: '1', letterSpacing: '0.05em', fontWeight: '600' }],
        'body-sm': ['14px', { lineHeight: '1.5', fontWeight: '400' }],
        'headline-lg': ['32px', { lineHeight: '1.2', letterSpacing: '-0.01em', fontWeight: '700' }],
        'headline-lg-mobile': ['24px', { lineHeight: '1.2', fontWeight: '700' }],
        'display-lg': ['56px', { lineHeight: '1.1', letterSpacing: '-0.03em', fontWeight: '800' }],
        'body-lg': ['18px', { lineHeight: '1.6', fontWeight: '400' }],
        'headline-md': ['28px', { lineHeight: '1.3', fontWeight: '700' }],
        'body-md': ['16px', { lineHeight: '1.6', fontWeight: '400' }]
      }
    }
  },
  plugins: []
}
