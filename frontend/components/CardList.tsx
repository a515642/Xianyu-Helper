import React, { useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import type { Card, CardKind } from '../types';
import { getCards, createCard, updateCard, deleteCard, batchCreateCards, type CardBatchResult } from '../services/api';
import {
  CreditCard, Edit, FileDown, FileText, Image as ImageIcon, ListChecks, Loader2, Package, Plus,
  Save, Search, Upload, X,
} from 'lucide-react';
import { cardContentLabel, filterCards } from './cardListState';
import CardItems from './CardItems';

type CardForm = {
  name: string;
  kind: CardKind;
  description: string;
  enabled: boolean;
  delay_seconds: number;
};

const emptyCardForm = (): CardForm => ({ name: '', kind: 'card', description: '', enabled: true, delay_seconds: 0 });

export const kindMeta: Record<CardKind, { label: string; hint: string }> = {
  card: { label: '批量卡密', hint: '每行一条，发货按顺序消耗，每条只能用一次' },
  text: { label: '固定文本', hint: '固定内容可重复发送，只取第一条卡密' },
  image: { label: '固定图片', hint: '固定图片 URL 可重复发送，只取第一条卡密' },
};

const CardIcon = ({ kind }: { kind: CardKind }) => {
  switch (kind) {
    case 'text': return <FileText className="w-5 h-5 text-blue-500" />;
    case 'image': return <ImageIcon className="w-5 h-5 text-purple-500" />;
    default: return <CreditCard className="w-5 h-5 text-gray-500" />;
  }
};

const CardList: React.FC = () => {
  const [view, setView] = useState<'cards' | 'items'>('cards');
  const [cards, setCards] = useState<Card[]>([]);
  const [activeCard, setActiveCard] = useState<Card | null>(null);

  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showEditModal, setShowEditModal] = useState(false);
  const [cardForm, setCardForm] = useState<CardForm>(emptyCardForm);
  const [editingCard, setEditingCard] = useState<Card | null>(null);

  const [showBatchModal, setShowBatchModal] = useState(false);
  const [batchFile, setBatchFile] = useState<File | null>(null);
  const [batchResult, setBatchResult] = useState<CardBatchResult | { error: string } | null>(null);
  const [batchBusy, setBatchBusy] = useState(false);

  const [kindFilter, setKindFilter] = useState<CardKind | ''>('');
  const [nameSearch, setNameSearch] = useState('');

  const reload = async () => setCards(await getCards());

  useEffect(() => {
    reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const filteredCards = useMemo(
    () => filterCards(cards, kindFilter, nameSearch),
    [cards, nameSearch, kindFilter],
  );

  const handleCreateCard = async () => {
    if (!cardForm.name.trim()) {
      alert('请输入卡种名称');
      return;
    }
    try {
      await createCard({
        name: cardForm.name.trim(),
        kind: cardForm.kind,
        description: cardForm.description.trim(),
        enabled: cardForm.enabled,
        delay_seconds: cardForm.delay_seconds || 0,
      });
      setShowCreateModal(false);
      setCardForm(emptyCardForm());
      await reload();
    } catch (error: any) {
      alert('创建失败：' + (error?.message || '请重试'));
    }
  };

  const openEdit = (card: Card) => {
    setEditingCard(card);
    setCardForm({
      name: card.name || '',
      kind: card.kind || 'card',
      description: card.description || '',
      enabled: card.enabled,
      delay_seconds: card.delay_seconds || 0,
    });
    setShowEditModal(true);
  };

  const handleSaveEdit = async () => {
    if (!editingCard) return;
    if (!cardForm.name.trim()) {
      alert('请输入卡种名称');
      return;
    }
    try {
      await updateCard(editingCard.id, {
        name: cardForm.name.trim(),
        kind: cardForm.kind,
        description: cardForm.description.trim(),
        enabled: cardForm.enabled,
        delay_seconds: cardForm.delay_seconds || 0,
      });
      setShowEditModal(false);
      setEditingCard(null);
      await reload();
    } catch (error: any) {
      alert('更新失败：' + (error?.message || '请重试'));
    }
  };

  const handleDelete = async (card: Card) => {
    if (!confirm(`确认删除卡种「${card.name}」？其下所有卡密会一并删除，且不可恢复。`)) return;
    try {
      await deleteCard(card.id);
      await reload();
    } catch (error: any) {
      alert('删除失败：' + (error?.message || '请重试'));
    }
  };

  const toggleCardStatus = async (card: Card) => {
    try {
      await updateCard(card.id, { ...card, enabled: !card.enabled });
      await reload();
    } catch (error: any) {
      alert('切换状态失败：' + (error?.message || '请重试'));
    }
  };

  const openManage = (card: Card) => {
    setActiveCard(card);
    setView('items');
  };

  const copyCardID = async (id: string | number) => {
    try {
      await navigator.clipboard.writeText(String(id));
      alert(`已复制卡种ID：${id}`);
    } catch {
      prompt('复制卡种ID', String(id));
    }
  };

  const downloadCardTemplate = () => {
    const headers = ['卡种', '类型', '内容', '描述', '启用', '延迟秒', '多规格', '规格名', '规格值'];
    const rows = [
      ['VIP月卡', 'card', 'VIP-MONTH-001', '每行一条卡密；同名卡种会合并', '是', '0', '否', '', ''],
      ['VIP月卡', 'card', 'VIP-MONTH-002', '', '', '', '', '', ''],
      ['VIP月卡', 'card', 'VIP-MONTH-003', '', '', '', '', '', ''],
      ['感谢文案', 'text', '感谢购买，如有问题联系客服～', '固定文本，可重复发送', '是', '0', '否', '', ''],
      ['教程图', 'image', 'https://cdn.example.com/tutorial.jpg', '固定图片，可重复发送', '是', '0', '否', '', ''],
      ['仅建卡种', '', '', '留空类型与内容，只创建卡种', '是', '0', '否', '', ''],
    ];
    const csv = [headers, ...rows]
      .map(row => row.map(cell => `"${String(cell).replace(/"/g, '""')}"`).join(','))
      .join('\n');
    const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = '卡种批量导入模板.csv';
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };

  const handleBatchCreate = async () => {
    if (!batchFile) return;
    setBatchBusy(true);
    setBatchResult(null);
    try {
      const res = await batchCreateCards(batchFile);
      setBatchResult(res);
      if (res.created > 0 || res.appended > 0) await reload();
    } catch (e: any) {
      setBatchResult({ error: e?.message || '上传失败' });
    } finally {
      setBatchBusy(false);
    }
  };

  if (view === 'items' && activeCard) {
    return (
      <CardItems
        card={activeCard}
        onBack={() => {
          setView('cards');
          setActiveCard(null);
          reload();
        }}
      />
    );
  }

  const kindTabs: { value: CardKind | ''; label: string }[] = [
    { value: '', label: '全部' },
    { value: 'card', label: '批量卡密' },
    { value: 'text', label: '固定文本' },
    { value: 'image', label: '固定图片' },
  ];

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col md:flex-row justify-between md:items-end gap-4">
        <div>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">卡密库存</h2>
          <p className="text-gray-500 mt-2 font-medium">卡种定义卡密类型与发货时机；卡密内容进入卡种后逐条管理。</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => { setBatchFile(null); setBatchResult(null); setShowBatchModal(true); }}
            className="px-4 py-3 bg-gray-100 hover:bg-gray-200 rounded-2xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
          >
            <Upload className="w-5 h-5" />
            批量导入
          </button>
          <button
            onClick={() => { setCardForm(emptyCardForm()); setShowCreateModal(true); }}
            className="ios-btn-primary flex items-center gap-2 px-6 py-3 rounded-2xl font-bold shadow-lg shadow-blue-200 transition-transform hover:scale-105 active:scale-95"
          >
            <Plus className="w-5 h-5" />
            创建卡种
          </button>
        </div>
      </div>

      <div className="ios-card rounded-xl overflow-hidden shadow-lg border-0 bg-white">
        <div className="flex flex-col gap-3 border-b border-gray-50 bg-surface-muted p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-1 flex-col gap-3 sm:flex-row sm:items-center">
            <div className="flex items-center gap-1 rounded-xl bg-white p-1 shadow-sm">
              {kindTabs.map(tab => (
                <button
                  key={tab.value || 'all'}
                  onClick={() => setKindFilter(tab.value)}
                  className={`rounded-lg px-3 py-1.5 text-xs font-bold transition-colors ${kindFilter === tab.value ? 'bg-brand text-white' : 'text-gray-500 hover:bg-gray-50'}`}
                >
                  {tab.label}
                </button>
              ))}
            </div>
            <div className="relative w-full sm:max-w-sm">
              <Search className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
              <input
                type="search"
                aria-label="按卡种名称搜索"
                placeholder="搜索卡种名称..."
                value={nameSearch}
                onChange={event => setNameSearch(event.target.value)}
                className="ios-input w-full rounded-xl border-none bg-white py-2.5 pl-10 pr-4 text-sm shadow-sm"
              />
            </div>
          </div>
          <div className="whitespace-nowrap px-1 text-xs font-bold text-gray-400">
            显示 {filteredCards.length} / {cards.length} 个卡种
          </div>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full table-fixed text-left border-collapse">
            <thead>
              <tr className="bg-white text-gray-400 text-xs font-bold uppercase tracking-wider border-b border-gray-50">
                <th className="w-[26%] px-5 py-5">卡种名称</th>
                <th className="w-[9%] px-3 py-5">卡种ID</th>
                <th className="w-[10%] px-3 py-5">卡密类型</th>
                <th className="w-[22%] px-5 py-5">内容 / 库存</th>
                <th className="w-[17%] px-5 py-5">描述</th>
                <th className="w-[8%] px-2 py-5">状态</th>
                <th className="w-[8%] px-3 py-5 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-50">
              {filteredCards.map(card => (
                <tr key={card.id} className="hover:bg-warning-50/50 transition-colors group">
                  <td className="px-5 py-5 align-middle">
                    <div className="flex items-center gap-2.5">
                      <div className="shrink-0 rounded-xl bg-gray-50 p-2 transition-colors group-hover:bg-white">
                        <CardIcon kind={card.kind} />
                      </div>
                      <span className="line-clamp-3 min-w-0 text-[13px] font-bold leading-5 text-gray-900" title={card.name}>{card.name}</span>
                    </div>
                  </td>
                  <td className="px-3 py-5">
                    <button
                      onClick={() => copyCardID(card.id)}
                      className="inline-flex max-w-full items-center gap-1 rounded-lg bg-gray-100 px-2 py-1.5 font-mono text-[11px] font-extrabold text-gray-700 transition-colors hover:bg-gray-200"
                      title="复制卡种ID，用于批量铺货表格"
                    >
                      <span className="truncate">{card.id}</span>
                    </button>
                  </td>
                  <td className="px-3 py-5">
                    <span className={`inline-flex rounded-md px-2 py-1 text-[11px] font-bold ${
                      card.kind === 'text' ? 'bg-blue-50 text-blue-600' :
                      card.kind === 'image' ? 'bg-purple-50 text-purple-600' :
                      'bg-gray-100 text-gray-600'
                    }`}>
                      {kindMeta[card.kind]?.label || '批量卡密'}
                    </span>
                  </td>
                  <td className="px-5 py-5">
                    <span className="break-all font-mono text-sm leading-5 text-gray-600">
                      {cardContentLabel(card)}
                    </span>
                  </td>
                  <td className="px-5 py-5">
                    <span className="block truncate text-sm text-gray-500" title={card.description || '-'}>
                      {card.description || '-'}
                    </span>
                  </td>
                  <td className="px-2 py-5">
                    <button
                      onClick={() => toggleCardStatus(card)}
                      role="switch"
                      aria-checked={card.enabled}
                      className={`relative inline-flex h-7 w-12 shrink-0 items-center rounded-full transition-colors ${card.enabled ? 'bg-green-500' : 'bg-gray-300'}`}
                    >
                      <span className={`absolute left-0.5 top-0.5 h-6 w-6 rounded-full bg-white shadow-sm transition-transform ${card.enabled ? 'translate-x-5' : 'translate-x-0'}`} />
                    </button>
                  </td>
                  <td className="px-3 py-5">
                    <div className="flex items-center justify-end gap-0.5">
                      <button
                        onClick={() => openManage(card)}
                        className="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-black"
                        title="管理卡密"
                      >
                        <ListChecks className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => openEdit(card)}
                        className="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-black"
                        title="编辑卡种"
                      >
                        <Edit className="w-4 h-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {filteredCards.length === 0 && (
          <div className="py-20 text-center text-gray-400">
            <Package className="w-12 h-12 mx-auto mb-4 opacity-30" />
            <p>{cards.length === 0 ? '暂无卡种，请点击右上角「创建卡种」。' : '没有符合当前筛选条件的卡种。'}</p>
          </div>
        )}
      </div>

      {/* 创建卡种 */}
      {showCreateModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{ maxWidth: '560px' }}>
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 className="text-2xl font-extrabold text-gray-900">创建卡种</h3>
                <p className="text-sm text-gray-500 mt-1">先确定卡密类型，卡密内容进入卡种后添加</p>
              </div>
              <button onClick={() => setShowCreateModal(false)} className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0" title="关闭">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>
            <div className="modal-body">
              <CardFormFields form={cardForm} onChange={setCardForm} />
            </div>
            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button onClick={() => setShowCreateModal(false)} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors">
                  取消
                </button>
                <button onClick={handleCreateCard} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2">
                  <Plus className="w-4 h-4" />
                  创建
                </button>
              </div>
            </div>
          </div>
        </div>, document.body)}

      {/* 编辑卡种 */}
      {showEditModal && editingCard && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{ maxWidth: '560px' }}>
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 className="text-2xl font-extrabold text-gray-900">编辑卡种</h3>
                <p className="text-sm text-gray-500 mt-1">{editingCard.name}</p>
              </div>
              <button onClick={() => setShowEditModal(false)} className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0" title="关闭">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>
            <div className="modal-body">
              <CardFormFields form={cardForm} onChange={setCardForm} />
            </div>
            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button onClick={() => setShowEditModal(false)} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors">
                  取消
                </button>
                <button onClick={handleSaveEdit} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2">
                  <Save className="w-4 h-4" />
                  保存更改
                </button>
              </div>
            </div>
          </div>
        </div>, document.body)}

      {/* 批量导入 */}
      {showBatchModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{ maxWidth: '640px' }}>
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 className="text-2xl font-extrabold text-gray-900">批量导入卡密</h3>
                <p className="text-sm text-gray-500 mt-1">每行一条卡密；同一「卡种」名称的行会合并到同一个卡种</p>
              </div>
              <button onClick={() => setShowBatchModal(false)} className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0" title="关闭">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>
            <div className="modal-body space-y-4">
              <button onClick={downloadCardTemplate} className="inline-flex items-center gap-2 rounded-xl bg-gray-100 px-4 py-2.5 text-sm font-bold text-gray-700 hover:bg-gray-200">
                <FileDown className="w-4 h-4" />
                下载模板
              </button>
              <input
                type="file"
                accept=".csv,.xlsx,.xls"
                onChange={e => setBatchFile(e.target.files?.[0] || null)}
                className="w-full ios-input px-4 py-3 rounded-xl"
              />
              <p className="text-xs text-gray-500">
                支持 CSV / XLSX。同一卡种名称的行会合并；卡种已存在时直接追加卡密。类型与内容同时留空的行只用于创建卡种。
              </p>
              {batchResult && (
                <div className="space-y-2 text-sm">
                  {'error' in batchResult ? (
                    <div className="rounded-xl bg-red-50 p-3 text-red-600">{batchResult.error}</div>
                  ) : (
                    <>
                      <div className="rounded-xl bg-gray-50 p-3 text-gray-600">
                        共 {batchResult.rows} 行 · 新建卡种 {batchResult.created} · 追加卡种 {batchResult.appended} · 写入卡密 {batchResult.items} 条 · 失败 {batchResult.failed} 行
                      </div>
                      {batchResult.groups.length > 0 && (
                        <div className="max-h-48 overflow-y-auto rounded-xl border border-gray-100">
                          {batchResult.groups.map(group => (
                            <div key={group.name} className="flex items-center justify-between border-b border-gray-50 px-3 py-2 last:border-0">
                              <span className="truncate font-bold text-gray-700" title={group.name}>{group.name}</span>
                              <span className="ml-3 shrink-0 text-xs text-gray-400">
                                {group.created ? '新建' : group.card_id ? '追加' : '未创建'}
                                {group.type ? ` · ${group.type}` : ''}
                                {group.added > 0 ? ` · +${group.added}` : ''}
                                {group.failed > 0 ? ` · 失败 ${group.failed}` : ''}
                              </span>
                            </div>
                          ))}
                        </div>
                      )}
                      {batchResult.errors.length > 0 && (
                        <div className="max-h-40 overflow-y-auto rounded-xl bg-red-50 p-3 text-xs text-red-600">
                          {batchResult.errors.map((item, index) => (
                            <div key={index}>第 {item.row_no} 行{item.name ? `（${item.name}）` : ''}：{item.error}</div>
                          ))}
                        </div>
                      )}
                    </>
                  )}
                </div>
              )}
            </div>
            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button onClick={() => setShowBatchModal(false)} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors">
                  关闭
                </button>
                <button onClick={handleBatchCreate} disabled={!batchFile || batchBusy} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2 disabled:opacity-50">
                  {batchBusy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Upload className="w-4 h-4" />}
                  导入
                </button>
              </div>
            </div>
          </div>
        </div>, document.body)}
    </div>
  );
};

const CardFormFields: React.FC<{ form: CardForm; onChange: (next: CardForm) => void }> = ({ form, onChange }) => (
  <div className="space-y-5">
    <div className="space-y-2">
      <label className="block text-sm font-bold text-gray-700">卡种名称</label>
      <input
        type="text"
        value={form.name}
        onChange={e => onChange({ ...form, name: e.target.value })}
        placeholder="例如：VIP会员卡密"
        className="w-full ios-input px-4 py-3 rounded-xl"
      />
    </div>
    <div className="space-y-2">
      <label className="block text-sm font-bold text-gray-700">卡密类型</label>
      <div className="grid grid-cols-3 gap-2">
        {(['card', 'text', 'image'] as CardKind[]).map(kind => (
          <button
            key={kind}
            type="button"
            onClick={() => onChange({ ...form, kind })}
            className={`rounded-xl p-3 text-sm font-bold transition-all ${
              form.kind === kind ? 'bg-brand text-white shadow-md' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
            }`}
          >
            {kind === 'card' ? <CreditCard className="mx-auto mb-1 h-5 w-5" /> :
             kind === 'text' ? <FileText className="mx-auto mb-1 h-5 w-5" /> :
             <ImageIcon className="mx-auto mb-1 h-5 w-5" />}
            {kindMeta[kind].label}
          </button>
        ))}
      </div>
      <p className="text-xs text-gray-500">{kindMeta[form.kind].hint}。</p>
    </div>
    <div className="space-y-2">
      <label className="block text-sm font-bold text-gray-700">延时发货时间（秒）</label>
      <input
        type="number"
        value={form.delay_seconds}
        min={0}
        max={3600}
        onChange={e => onChange({ ...form, delay_seconds: parseInt(e.target.value) || 0 })}
        className="w-full ios-input px-4 py-3 rounded-xl"
      />
      <p className="text-xs text-gray-500">0 表示立即发货，最大 3600 秒。</p>
    </div>
    <div className="space-y-2">
      <label className="block text-sm font-bold text-gray-700">描述</label>
      <textarea
        value={form.description}
        onChange={e => onChange({ ...form, description: e.target.value })}
        className="w-full ios-input px-4 py-3 rounded-xl h-24 resize-none"
        placeholder="可选的备注信息"
      />
    </div>
    <div className="flex items-center gap-3">
      <button
        type="button"
        role="switch"
        aria-checked={form.enabled}
        onClick={() => onChange({ ...form, enabled: !form.enabled })}
        className={`relative inline-flex h-7 w-12 shrink-0 items-center rounded-full transition-colors ${form.enabled ? 'bg-green-500' : 'bg-gray-300'}`}
      >
        <span className={`absolute left-0.5 top-0.5 h-6 w-6 rounded-full bg-white shadow-sm transition-transform ${form.enabled ? 'translate-x-5' : 'translate-x-0'}`} />
      </button>
      <span className="text-sm font-bold text-gray-600">{form.enabled ? '启用' : '停用'}</span>
    </div>
  </div>
);

export default CardList;
