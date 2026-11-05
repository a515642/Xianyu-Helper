import type { Card, CardKind } from '../types';

export const cardStockLabel = (stock?: { available?: number; used?: number; total?: number } | null): string => {
  if (!stock) return '可用 0 / 已用 0 / 共 0';
  return `可用 ${stock.available ?? 0} / 已用 ${stock.used ?? 0} / 共 ${stock.total ?? 0}`;
};

// 卡种内容摘要：批量卡密看库存，固定文本/图片看是否已配置。
export const cardContentLabel = (card: Card): string => {
  switch (card.kind) {
    case 'text':
      return card.item_count ? '固定文本（可重复发送）' : '固定文本（未配置）';
    case 'image':
      return card.item_count ? '固定图片（可重复发送）' : '固定图片（未配置）';
    default:
      return cardStockLabel(card.stock);
  }
};

export const filterCards = (
  cards: Card[],
  kindFilter: CardKind | '',
  nameSearch: string,
): Card[] => {
  const keyword = nameSearch.trim().toLocaleLowerCase();
  return cards.filter(card => (
    (!kindFilter || card.kind === kindFilter)
    && (!keyword || card.name.toLocaleLowerCase().includes(keyword))
  ));
};
