export const profiles = {
  test: { channel: 'test', label: '测试版', name: 'AiTok 助手测试版', appId: 'com.toktopup.assistant.test', origin: 'http://localhost:15680', port: 15684 },
  production: { channel: 'production', label: '线上版', name: 'AiTok 助手', appId: 'com.toktopup.assistant', origin: 'https://toktopup.com', port: 15683 },
};
export const profile = profiles[typeof __AITOK_CHANNEL__ === 'string' ? __AITOK_CHANNEL__ : 'test'];
