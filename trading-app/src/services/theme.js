// 科幻蓝 — Cyberpunk / HUD 终端风格
export const colors = {
  // 基础背景层次（深蓝黑→蓝灰，从深到浅）
  bg: '#0a0e14',
  card: '#0f1923',
  cardAlt: '#142030',
  cardBorder: '#1a3348',
  surface: '#121e2b',
  surfaceLight: '#1a2d3d',
  surfaceHover: '#223848',
  divider: '#1a3348',

  // 文字层次（冷白→蓝灰）
  text: '#c8dce8',
  textSecondary: '#7a9ab0',
  textMuted: '#4a6578',
  white: '#e0eaf2',

  // 主强调色 — 霓虹青（变量名保留 gold 以兼容全局引用）
  gold: '#00e5ff',
  goldLight: '#33ecff',
  goldDark: '#00a0b4',
  goldBg: 'rgba(0,229,255,0.08)',
  goldGlow: 'rgba(0,229,255,0.25)',

  // 多头/做多（科技绿，更亮）
  green: '#00e676',
  greenLight: '#69f0ae',
  greenBg: 'rgba(0,230,118,0.10)',
  greenBgSolid: '#0a2418',
  greenGlow: 'rgba(0,230,118,0.25)',

  // 空头/做空（霓虹红）
  red: '#ff3b5c',
  redLight: '#ff6b81',
  redBg: 'rgba(255,59,92,0.10)',
  redBgSolid: '#1f0a10',
  redGlow: 'rgba(255,59,92,0.25)',

  // 信息蓝（主蓝）
  blue: '#448aff',
  blueLight: '#6ea6ff',
  blueDark: '#2962ff',
  blueBg: 'rgba(68,138,255,0.12)',
  blueGlow: 'rgba(68,138,255,0.3)',

  // 辅助色
  yellow: '#ffb800',
  yellowBg: 'rgba(255,184,0,0.10)',

  orange: '#ff9100',
  orangeBg: 'rgba(255,145,0,0.10)',

  purple: '#7b61ff',
  purpleBg: 'rgba(123,97,255,0.12)',

  // 阴影
  shadow: 'rgba(0, 4, 12, 0.9)',
};

// 间距规范
export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 20,
  xxl: 28,
};

// 圆角规范
export const radius = {
  xs: 4,
  sm: 6,
  md: 10,
  lg: 14,
  xl: 18,
  xxl: 24,
  pill: 999,
};

// 字号规范
export const fontSize = {
  xs: 10,
  sm: 12,
  md: 14,
  lg: 16,
  xl: 20,
  xxl: 26,
  hero: 32,
};
