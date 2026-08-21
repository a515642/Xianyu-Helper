import { describe, expect, test } from 'vitest';
import { clipboardImageFile, maxChatImageBytes, validateChatImage } from './chatImage';

const file = (name: string, type: string, size: number): File =>
  new File([new Uint8Array(size)], name, { type });

describe('chat image helpers', () => {
  test('accepts non-empty images up to 10MB', () => {
    expect(validateChatImage(file('paste.png', 'image/png', 1))).toBeNull();
    expect(validateChatImage(file('large.jpg', 'image/jpeg', maxChatImageBytes))).toBeNull();
  });

  test('rejects empty, non-image, and oversized files', () => {
    expect(validateChatImage(file('empty.png', 'image/png', 0))).toBe('图片不能为空');
    expect(validateChatImage(file('payload.txt', 'text/plain', 1))).toBe('只能发送图片文件');
    expect(validateChatImage(file('large.png', 'image/png', maxChatImageBytes + 1))).toBe('图片不能超过 10MB');
  });

  test('prefers the first image clipboard item', () => {
    const image = file('clipboard.png', 'image/png', 2);
    const text = { kind: 'string', type: 'text/plain', getAsFile: () => null } as unknown as DataTransferItem;
    const imageItem = { kind: 'file', type: 'image/png', getAsFile: () => image } as unknown as DataTransferItem;
    const clipboard = { items: [text, imageItem], files: [] } as unknown as DataTransfer;
    expect(clipboardImageFile(clipboard)).toBe(image);
  });

  test('falls back to clipboard files', () => {
    const image = file('clipboard.jpg', 'image/jpeg', 2);
    const clipboard = { items: [], files: [image] } as unknown as DataTransfer;
    expect(clipboardImageFile(clipboard)).toBe(image);
  });
});
