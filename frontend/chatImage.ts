export const maxChatImageBytes = 10 * 1024 * 1024;

export const validateChatImage = (file: File): string | null => {
  if (!file.type.toLowerCase().startsWith('image/')) {
    return '只能发送图片文件';
  }
  if (file.size > maxChatImageBytes) {
    return '图片不能超过 10MB';
  }
  if (file.size === 0) {
    return '图片不能为空';
  }
  return null;
};

export const clipboardImageFile = (clipboard: DataTransfer | null): File | undefined => {
  if (!clipboard) return undefined;

  for (const item of Array.from(clipboard.items)) {
    if (item.kind !== 'file' || !item.type.toLowerCase().startsWith('image/')) continue;
    const file = item.getAsFile();
    if (file) return file;
  }

  return Array.from(clipboard.files).find(file => file.type.toLowerCase().startsWith('image/'));
};
