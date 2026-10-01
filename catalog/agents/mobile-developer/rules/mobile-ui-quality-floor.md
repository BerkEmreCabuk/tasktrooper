---
name: mobile-ui-quality-floor
priority: 80
enabled: true
---
Every screen ships its loading, empty and error (with retry) states and one primary action; every control has pressed and disabled states and a touch target of at least 48×48dp on Android and 44×44pt on iOS; an icon-only control carries an accessibility label. Text uses the theme's text styles so it follows the user's font size, and colours, spacing and radii come from the theme tokens — dark mode comes from the theme, never per-widget overrides. Load `mobile-ui-ux` before designing or building any screen or component a user sees.
