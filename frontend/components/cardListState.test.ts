import { describe, expect, test } from 'vitest';
import type { Card } from '../types';
import { cardContentLabel, cardStockLabel, filterCards } from './cardListState';

const cards = [
  { id: 1, name: '年终总结 PPT', kind: 'text', item_count: 1 },
  { id: 2, name: '年终总结兑换码', kind: 'card' },
  { id: 3, name: '产品介绍 PPT', kind: 'text', item_count: 1 },
  { id: 4, name: '空图片卡种', kind: 'image' },
] as Card[];

describe('filterCards', () => {
  test('combines kind and name filters with AND semantics', () => {
    expect(filterCards(cards, 'text', '年终')).toEqual([cards[0]]);
  });

  test('matches names case-insensitively and ignores surrounding whitespace', () => {
    expect(filterCards(cards, '', '  ppt ')).toEqual([cards[0], cards[2]]);
  });

  test('matches every card type when no kind filter is set', () => {
    expect(filterCards(cards, '', '空图片')).toEqual([cards[3]]);
  });

  test('filters by kind', () => {
    expect(filterCards(cards, 'card', '')).toEqual([cards[1]]);
    expect(filterCards(cards, 'image', '')).toEqual([cards[3]]);
  });
});

describe('cardStockLabel', () => {
  test('summarizes available, used and total', () => {
    expect(cardStockLabel({ available: 3, used: 2, total: 5 })).toBe('可用 3 / 已用 2 / 共 5');
  });

  test('falls back to zero when stock is missing', () => {
    expect(cardStockLabel(undefined)).toBe('可用 0 / 已用 0 / 共 0');
  });
});

describe('cardContentLabel', () => {
  test('describes fixed text and image card types', () => {
    expect(cardContentLabel({ id: 1, name: '文案', kind: 'text', item_count: 1 } as Card)).toBe('固定文本（可重复发送）');
    expect(cardContentLabel({ id: 9, name: '图', kind: 'image', item_count: 1 } as Card)).toBe('固定图片（可重复发送）');
  });

  test('marks fixed types without content', () => {
    expect(cardContentLabel({ id: 8, name: '空图', kind: 'image' } as Card)).toBe('固定图片（未配置）');
  });

  test('shows stock for card type', () => {
    expect(cardContentLabel({ id: 2, name: '批量', kind: 'card', stock: { card_id: 2, total: 5, available: 3, used: 2, void: 0 } } as Card))
      .toBe('可用 3 / 已用 2 / 共 5');
  });
});
