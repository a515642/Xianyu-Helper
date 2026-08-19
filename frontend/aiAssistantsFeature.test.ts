import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const source = (path: string) => readFileSync(resolve(__dirname, path), 'utf8');

describe('product scoped AI assistants', () => {
  test('exposes a dedicated routed management page', () => {
    const app = source('App.tsx');
    const sidebar = source('components/Sidebar.tsx');
    expect(app).toContain("'/app/ai-assistants': 'ai-assistants'");
    expect(app).toContain("case 'ai-assistants': return <AIAssistants isAdmin={isAdmin} />");
    expect(sidebar).toContain("id: 'ai-assistants'");
  });

  test('supports account isolation, item multi-select, custom API and global forbidden words', () => {
    const page = source('components/AIAssistants.tsx');
    expect(page).toContain('getAIProfiles(id)');
    expect(page).toContain('getItems(id)');
    expect(page).toContain('use_system_api');
    expect(page).toContain('API Key（留空保持）');
    expect(page).toContain('item_ids');
    expect(page).toContain('全局违禁词');
    expect(page).toContain('replaceAIForbiddenWords');
    expect(page).toContain('fetchAIModels');
    expect(page).toContain('thinking_mode');
    expect(page).toContain('{{item_title}}');
    expect(page).toContain('modal-header flex items-center justify-between');
    expect(page).toContain('flex w-full justify-end gap-3');
  });
});
