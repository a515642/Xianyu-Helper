import React, { useCallback, useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  ArrowDownToLine, ArrowUpFromLine, Ban, ChevronLeft, ChevronRight, Copy, Download,
  FileDown, Loader2, Package, Plus, RefreshCw, ScrollText, Search, SlidersHorizontal, Trash2, X,
} from 'lucide-react';
import {
  addCardItems, bulkUpdateCardItems, deleteCardItems, exportCardItems, exportCardSales, getCardItems, getCardSales, updateCardItem,
} from '../services/api';
import type { Card, CardItem, CardItemPage, CardItemStatus, CardSalePage, CardSaleStatus } from '../types';
import { formatLocalDateTime } from '../dateTime';
import { kindMeta } from './CardList';

type Props = {
  card: Card;
  onBack: () => void;
};

const statusMeta: Record<CardItemStatus, { label: string; className: string }> = {
  available: { label: '可用', className: 'bg-green-50 text-green-600' },
  used: { label: '已用', className: 'bg-blue-50 text-blue-600' },
  void: { label: '作废', className: 'bg-gray-100 text-gray-500' },
};

const emptyPage: CardItemPage = { items: [], total: 0, page: 1, page_size: 20, total_pages: 0 };
const emptySalePage: CardSalePage = { records: [], total: 0, page: 1, page_size: 20, total_pages: 0 };

const saleStatusMeta: Record<CardSaleStatus, { label: string; className: string }> = {
  reserved: { label: '待核对', className: 'bg-amber-50 text-amber-700' },
  sold: { label: '已售出', className: 'bg-emerald-50 text-emerald-700' },
};

const CardItems: React.FC<Props> = ({ card, onBack }) => {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState<CardItemPage>(emptyPage);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [copyTarget, setCopyTarget] = useState<CardItem | null>(null);

  const [copyList, setCopyList] = useState<CardItem[] | null>(null);
  const [exportBusy, setExportBusy] = useState(false);

  const [showAddModal, setShowAddModal] = useState(false);
  const [addContent, setAddContent] = useState('');
  const [addBusy, setAddBusy] = useState(false);

  const [statusFilter, setStatusFilter] = useState<CardItemStatus | ''>('');
  const [keyword, setKeyword] = useState('');
  const [buyerId, setBuyerId] = useState('');
  const [buyerName, setBuyerName] = useState('');
  const [orderId, setOrderId] = useState('');

  const [showSales, setShowSales] = useState(false);
  const [saleData, setSaleData] = useState<CardSalePage>(emptySalePage);
  const [saleStatus, setSaleStatus] = useState<CardSaleStatus | ''>('');
  const [saleSearch, setSaleSearch] = useState('');
  const [salePage, setSalePage] = useState(1);
  const [salesBusy, setSalesBusy] = useState(false);

  const isFixed = card.kind === 'text' || card.kind === 'image';

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const res = await getCardItems({
        page, page_size: pageSize,
        card_id: card.id,
        status: statusFilter,
        keyword, buyer_id: buyerId, buyer_name: buyerName, order_id: orderId,
      });
      setData(res);
      setSelected(new Set());
    } catch (error) {
      console.error('加载卡密失败:', error);
    } finally {
      setBusy(false);
    }
  }, [card.id, page, pageSize, statusFilter, keyword, buyerId, buyerName, orderId]);

  useEffect(() => { load(); }, [load]);

  const loadSales = useCallback(async () => {
    setSalesBusy(true);
    try {
      const res = await getCardSales({
        card_id: card.id, status: saleStatus, search: saleSearch, page: salePage, page_size: 20,
      });
      setSaleData(res);
    } catch (error) {
      console.error('加载售出台账失败:', error);
    } finally {
      setSalesBusy(false);
    }
  }, [card.id, saleStatus, saleSearch, salePage]);

  useEffect(() => {
    if (showSales) void loadSales();
  }, [showSales, loadSales]);

  const totalPages = Math.max(1, data.total_pages || 1);
  const saleTotalPages = Math.max(1, saleData.total_pages || 1);

  const handleExportSales = async () => {
    try {
      const blob = await exportCardSales(card.id, saleStatus);
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `${card.name}-售出.csv`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    } catch (error: any) {
      alert('导出售出台账失败：' + (error?.message || '请重试'));
    }
  };

  const resetFilters = () => {
    setStatusFilter(''); setKeyword('');
    setBuyerId(''); setBuyerName(''); setOrderId(''); setPage(1);
  };

  const pageIDs = data.items.map(item => item.id);
  const allSelected = pageIDs.length > 0 && pageIDs.every(id => selected.has(id));

  const toggleAll = () => {
    setSelected(prev => {
      const next = new Set(prev);
      if (allSelected) pageIDs.forEach(id => next.delete(id));
      else pageIDs.forEach(id => next.add(id));
      return next;
    });
  };

  const toggleOne = (id: number) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  const runBulk = async (status: CardItemStatus, label: string) => {
    const ids = Array.from(selected);
    if (ids.length === 0) { alert('请先选择卡密'); return; }
    if (!confirm(`确认${label}选中的 ${ids.length} 条卡密？`)) return;
    setBusy(true);
    try {
      const res = await bulkUpdateCardItems(ids, status);
      await load();
      if (res?.updated !== undefined && res.updated < ids.length) {
        alert(`已${label} ${res.updated} 条，其余状态不匹配已跳过。`);
      }
    } catch (error: any) {
      alert(`${label}失败：${error?.message || '未知错误'}`);
    } finally {
      setBusy(false);
    }
  };

  const openBulkCopy = () => {
    const ids = Array.from(selected);
    if (ids.length === 0) { alert('请先选择卡密'); return; }
    const picked: CardItem[] = [];
    const seen = new Set<number>();
    for (const item of data.items) {
      if (selected.has(item.id) && !seen.has(item.id)) { picked.push(item); seen.add(item.id); }
    }
    if (picked.length !== ids.length) {
      alert('请在当前页选择要复制的卡密');
      return;
    }
    setCopyList(picked);
  };

  const copyBatchText = async () => {
    if (!copyList) return;
    const text = copyList.map(item => item.content).join('\n');
    try {
      await navigator.clipboard.writeText(text);
      alert(`已复制 ${copyList.length} 条卡密`);
    } catch {
      prompt('复制卡密（可手动全选）', text);
    }
  };

  const handleExport = async (format: 'txt' | 'csv' | 'xlsx') => {
    const ids = Array.from(selected);
    setExportBusy(true);
    try {
      const blob = await exportCardItems(card.id, {
        format,
        ids: ids.length > 0 ? ids : undefined,
        status: ids.length > 0 ? undefined : statusFilter,
        keyword: ids.length > 0 ? undefined : keyword,
        buyer_id: ids.length > 0 ? undefined : buyerId,
        buyer_name: ids.length > 0 ? undefined : buyerName,
        order_id: ids.length > 0 ? undefined : orderId,
      });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `${card.name}.${format}`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    } catch (error: any) {
      alert('导出失败：' + (error?.message || '请重试'));
    } finally {
      setExportBusy(false);
    }
  };

  const copyContent = async () => {
    if (!copyTarget) return;
    try {
      await navigator.clipboard.writeText(copyTarget.content);
      alert('已复制卡密内容');
    } catch {
      // 剪贴板不可用时保持弹窗，用户可手动选择复制。
    }
  };

  const handleDelete = async () => {
    const ids = Array.from(selected);
    if (ids.length === 0) { alert('请先选择卡密'); return; }
    if (!confirm(`确认删除选中的 ${ids.length} 条卡密？该操作不可恢复。`)) return;
    setBusy(true);
    try {
      await deleteCardItems(ids);
      await load();
    } catch (error: any) {
      alert('删除失败：' + (error?.message || '未知错误'));
    } finally {
      setBusy(false);
    }
  };

  const handleEditContent = async (item: CardItem) => {
    const next = prompt('修改卡密内容', item.content);
    if (next === null) return;
    const content = next.trim();
    if (!content) { alert('卡密内容不能为空'); return; }
    try {
      await updateCardItem(item.id, { content });
      await load();
    } catch (error: any) {
      alert('更新失败：' + (error?.message || '未知错误'));
    }
  };

  const openAdd = () => { setAddContent(''); setShowAddModal(true); };

  const handleAdd = async () => {
    if (!addContent.trim()) { alert('请输入卡密内容'); return; }
    setAddBusy(true);
    try {
      await addCardItems(card.id, addContent);
      setShowAddModal(false);
      setAddContent('');
      await load();
    } catch (error: any) {
      alert('添加卡密失败：' + (error?.message || '请重试'));
    } finally {
      setAddBusy(false);
    }
  };

  const addPlaceholder = card.kind === 'image'
    ? 'https://example.com/card.png'
    : card.kind === 'text'
      ? '请输入每次发货时发送的固定文本'
      : 'CODE-123456';

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col md:flex-row justify-between md:items-end gap-4">
        <div>
          <button
            onClick={onBack}
            className="mb-2 inline-flex items-center gap-1 text-xs font-bold text-gray-400 hover:text-gray-600"
          >
            <ChevronLeft className="h-4 w-4" /> 返回卡种列表
          </button>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">{card.name}</h2>
          <p className="text-gray-500 mt-2 font-medium">
            卡种 ID {card.id} · {kindMeta[card.kind]?.label} · 共 {data.total} 条
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => { setSalePage(1); setShowSales(true); }}
            className="px-4 py-3 bg-gray-100 hover:bg-gray-200 rounded-2xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
          >
            <ScrollText className="w-5 h-5" />
            售出台账
          </button>
          <button
            onClick={load}
            className="px-4 py-3 bg-gray-100 hover:bg-gray-200 rounded-2xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
          >
            <RefreshCw className={`w-5 h-5 ${busy ? 'animate-spin' : ''}`} />
            刷新
          </button>
          <button
            onClick={openAdd}
            disabled={isFixed && data.total > 0}
            title={isFixed && data.total > 0 ? '固定文本/图片卡种只能有一条卡密' : '添加卡密'}
            className="ios-btn-primary px-6 py-3 rounded-2xl font-bold shadow-lg shadow-blue-200 transition-transform hover:scale-105 active:scale-95 flex items-center gap-2 disabled:opacity-40 disabled:hover:scale-100"
          >
            <Plus className="w-5 h-5" />
            添加卡密
          </button>
        </div>
      </div>

      {isFixed && (
        <div className="rounded-xl bg-amber-50 px-4 py-3 text-sm text-amber-700">
          {kindMeta[card.kind].hint}。只需要一条卡密，发货时重复发送这条内容。
        </div>
      )}

      <div className="ios-card rounded-xl bg-white p-4 shadow-lg border-0">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <label className="mb-1 block text-xs font-bold text-gray-400">状态</label>
            <select
              value={statusFilter}
              onChange={e => { setStatusFilter(e.target.value as CardItemStatus | ''); setPage(1); }}
              className="ios-input w-full rounded-xl border-none bg-white py-2.5 px-3 text-sm shadow-sm"
            >
              <option value="">全部状态</option>
              <option value="available">可用</option>
              <option value="used">已用</option>
              <option value="void">作废</option>
            </select>
          </div>
          <div>
            <label className="mb-1 block text-xs font-bold text-gray-400">卡密内容</label>
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
              <input
                value={keyword}
                onChange={e => { setKeyword(e.target.value); setPage(1); }}
                placeholder="搜索卡密内容..."
                className="ios-input w-full rounded-xl border-none bg-white py-2.5 pl-9 pr-3 text-sm shadow-sm"
              />
            </div>
          </div>
          <div>
            <label className="mb-1 block text-xs font-bold text-gray-400">买家（ID / 昵称）</label>
            <div className="flex gap-2">
              <input
                value={buyerId}
                onChange={e => { setBuyerId(e.target.value); setPage(1); }}
                placeholder="买家 ID"
                className="ios-input w-full rounded-xl border-none bg-white py-2.5 px-3 text-sm shadow-sm"
              />
              <input
                value={buyerName}
                onChange={e => { setBuyerName(e.target.value); setPage(1); }}
                placeholder="昵称"
                className="ios-input w-full rounded-xl border-none bg-white py-2.5 px-3 text-sm shadow-sm"
              />
            </div>
          </div>
          <div>
            <label className="mb-1 block text-xs font-bold text-gray-400">订单号</label>
            <input
              value={orderId}
              onChange={e => { setOrderId(e.target.value); setPage(1); }}
              placeholder="按订单号查询"
              className="ios-input w-full rounded-xl border-none bg-white py-2.5 px-3 text-sm shadow-sm"
            />
          </div>
        </div>
        <div className="mt-3 flex items-center justify-between">
          <button onClick={resetFilters} className="text-xs font-bold text-gray-400 hover:text-gray-600">
            重置筛选
          </button>
          <div className="flex items-center gap-2 text-xs font-bold text-gray-400">
            <SlidersHorizontal className="h-4 w-4" />
            共 {data.total} 条
          </div>
        </div>
      </div>

      <div className="ios-card rounded-xl overflow-hidden shadow-lg border-0 bg-white">
        <div className="flex flex-wrap items-center gap-2 border-b border-gray-50 bg-surface-muted p-4">
          <span className="mr-2 text-xs font-bold text-gray-500">已选 {selected.size} 条</span>
          <button onClick={() => runBulk('available', '回库')}
            className="inline-flex items-center gap-1 rounded-xl bg-green-50 px-3 py-2 text-xs font-bold text-green-600 hover:bg-green-100">
            <ArrowUpFromLine className="h-4 w-4" /> 回库（标记可用）
          </button>
          <button onClick={() => runBulk('used', '出库')}
            className="inline-flex items-center gap-1 rounded-xl bg-blue-50 px-3 py-2 text-xs font-bold text-blue-600 hover:bg-blue-100">
            <ArrowDownToLine className="h-4 w-4" /> 出库（标记已用）
          </button>
          <button onClick={() => runBulk('void', '作废')}
            className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 hover:bg-gray-200">
            <Ban className="h-4 w-4" /> 作废
          </button>
          <button onClick={handleDelete}
            className="inline-flex items-center gap-1 rounded-xl bg-red-50 px-3 py-2 text-xs font-bold text-red-500 hover:bg-red-100">
            <Trash2 className="h-4 w-4" /> 删除
          </button>
          <span className="mx-1 hidden h-5 w-px bg-gray-200 sm:block" />
          <button onClick={openBulkCopy}
            className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 hover:bg-gray-200">
            <Copy className="h-4 w-4" /> 批量复制
          </button>
          <button onClick={() => handleExport('txt')} disabled={exportBusy}
            className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 hover:bg-gray-200 disabled:opacity-50">
            {exportBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <FileDown className="h-4 w-4" />} 导出TXT
          </button>
          <button onClick={() => handleExport('csv')} disabled={exportBusy}
            className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 hover:bg-gray-200 disabled:opacity-50">
            <Download className="h-4 w-4" /> CSV
          </button>
          <button onClick={() => handleExport('xlsx')} disabled={exportBusy}
            className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 hover:bg-gray-200 disabled:opacity-50">
            <Download className="h-4 w-4" /> XLSX
          </button>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full table-fixed text-left border-collapse">
            <thead>
              <tr className="bg-white text-gray-400 text-xs font-bold uppercase tracking-wider border-b border-gray-50">
                <th className="w-[5%] px-3 py-4">
                  <input type="checkbox" aria-label="全选本页" checked={allSelected} onChange={toggleAll} />
                </th>
                <th className="w-[8%] px-3 py-4">ID</th>
                <th className="w-[30%] px-3 py-4">卡密内容</th>
                <th className="w-[9%] px-3 py-4">状态</th>
                <th className="w-[18%] px-3 py-4">买家 / 订单</th>
                <th className="w-[16%] px-3 py-4">售出时间</th>
                <th className="w-[14%] px-3 py-4 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-50">
              {data.items.map(item => {
                const meta = statusMeta[item.status] ?? statusMeta.void;
                return (
                  <tr key={item.id} className="hover:bg-warning-50/40 transition-colors">
                    <td className="px-3 py-4 align-middle">
                      <input type="checkbox" aria-label={`选择卡密 ${item.id}`} checked={selected.has(item.id)} onChange={() => toggleOne(item.id)} />
                    </td>
                    <td className="px-3 py-4 font-mono text-xs font-bold text-gray-500">{item.id}</td>
                    <td className="px-3 py-4">
                      <span className="line-clamp-2 break-all font-mono text-sm leading-5 text-gray-600" title={item.content}>{item.content}</span>
                    </td>
                    <td className="px-3 py-4">
                      <span className={`inline-flex rounded-md px-2 py-1 text-[11px] font-bold ${meta.className}`}>{meta.label}</span>
                    </td>
                    <td className="px-3 py-4 text-xs text-gray-500">
                      <div className="truncate" title={item.buyer_id}>{item.buyer_name || item.buyer_id || '-'}</div>
                      <div className="truncate font-mono text-gray-400" title={item.order_id}>{item.order_id || '-'}</div>
                    </td>
                    <td className="px-3 py-4 text-xs text-gray-400">{item.sold_at || '-'}</td>
                    <td className="px-3 py-4 text-right">
                      <div className="flex items-center justify-end gap-0.5">
                        <button onClick={() => setCopyTarget(item)} title="复制卡密"
                          className="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-black">
                          <Copy className="h-4 w-4" />
                        </button>
                        <button onClick={() => handleEditContent(item)} title="修改内容"
                          className="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-black">
                          <SlidersHorizontal className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
              {data.items.length === 0 && (
                <tr>
                  <td colSpan={7} className="py-16 text-center text-gray-400">
                    <Package className="w-12 h-12 mx-auto mb-4 opacity-30" />
                    <p>{busy ? '加载中…' : '该卡种还没有卡密，点击右上角「添加卡密」录入。'}</p>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <div className="flex flex-col items-center justify-between gap-3 border-t border-gray-50 p-4 sm:flex-row">
          <div className="flex items-center gap-2 text-xs font-bold text-gray-400">
            每页
            <select
              value={pageSize}
              onChange={e => { setPageSize(Number(e.target.value)); setPage(1); }}
              className="rounded-lg border-none bg-white py-1.5 px-2 text-xs shadow-sm"
            >
              {[10, 20, 50, 100].map(size => <option key={size} value={size}>{size}</option>)}
            </select>
            条
          </div>
          <div className="flex items-center gap-2">
            <button onClick={() => setPage(p => Math.max(1, p - 1))} disabled={page <= 1 || busy}
              className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 disabled:opacity-40">
              <ChevronLeft className="h-4 w-4" /> 上一页
            </button>
            <span className="text-xs font-bold text-gray-500">{page} / {totalPages}</span>
            <button onClick={() => setPage(p => Math.min(totalPages, p + 1))} disabled={page >= totalPages || busy}
              className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 disabled:opacity-40">
              下一页 <ChevronRight className="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>

      {/* 添加卡密 */}
      {showAddModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{ maxWidth: '600px' }}>
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 className="text-2xl font-extrabold text-gray-900">添加卡密</h3>
                <p className="text-sm text-gray-500 mt-1">{card.name} · {kindMeta[card.kind]?.label}</p>
              </div>
              <button onClick={() => setShowAddModal(false)} className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0" title="关闭">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>
            <div className="modal-body space-y-3">
              <label className="block text-sm font-bold text-gray-700">
                {card.kind === 'card' ? '卡密内容（一行一条）' : card.kind === 'image' ? '图片 URL' : '固定文本'}
              </label>
              {card.kind === 'image' ? (
                <input
                  type="url"
                  value={addContent}
                  onChange={e => setAddContent(e.target.value)}
                  placeholder={addPlaceholder}
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              ) : (
                <textarea
                  value={addContent}
                  onChange={e => setAddContent(e.target.value)}
                  placeholder={addPlaceholder}
                  className={`w-full ios-input px-4 py-3 rounded-xl resize-none text-sm ${card.kind === 'card' ? 'h-56 font-mono' : 'h-32'}`}
                />
              )}
              <p className="text-xs text-gray-500">{kindMeta[card.kind].hint}。</p>
            </div>
            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button onClick={() => setShowAddModal(false)} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors">
                  取消
                </button>
                <button onClick={handleAdd} disabled={addBusy} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2 disabled:opacity-50">
                  {addBusy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}
                  添加
                </button>
              </div>
            </div>
          </div>
        </div>, document.body)}

      {copyList && createPortal(
        <div className="modal-overlay-centered" onClick={() => setCopyList(null)}>
          <div className="modal-container" onClick={e => e.stopPropagation()} style={{ maxWidth: '640px' }}>
            <div className="modal-header flex items-center justify-between">
              <div>
                <h3 className="text-lg font-bold text-gray-900">复制卡密（{copyList.length} 条）</h3>
                <p className="text-xs text-gray-500 mt-1">只读，一行一条，方便直接复制到别处</p>
              </div>
              <button onClick={() => setCopyList(null)} className="rounded-lg p-1 text-gray-400 hover:bg-gray-100">
                <X className="h-5 w-5" />
              </button>
            </div>
            <div className="p-6 space-y-4">
              <textarea
                readOnly
                value={copyList.map(item => item.content).join('\n')}
                rows={10}
                className="ios-input w-full rounded-xl border-none bg-gray-50 p-3 font-mono text-sm text-gray-700"
                onFocus={e => e.currentTarget.select()}
              />
              <div className="flex justify-end gap-2">
                <button onClick={copyBatchText}
                  className="ios-btn-primary inline-flex items-center gap-1.5 px-4 py-2.5 rounded-xl font-bold">
                  <Copy className="h-4 w-4" /> 复制全部
                </button>
              </div>
            </div>
          </div>
        </div>, document.body)}

      {copyTarget && createPortal(
        <div className="modal-overlay-centered" onClick={() => setCopyTarget(null)}>
          <div className="modal-container" onClick={e => e.stopPropagation()}>
            <div className="modal-header flex items-center justify-between">
              <h3 className="text-lg font-bold text-gray-900">复制卡密</h3>
              <button onClick={() => setCopyTarget(null)} className="rounded-lg p-1 text-gray-400 hover:bg-gray-100">
                <X className="h-5 w-5" />
              </button>
            </div>
            <div className="p-6 space-y-4">
              <div className="grid grid-cols-2 gap-3 text-xs text-gray-500">
                <div><span className="font-bold text-gray-400">状态：</span>{statusMeta[copyTarget.status]?.label}</div>
                <div><span className="font-bold text-gray-400">买家：</span>{copyTarget.buyer_name || copyTarget.buyer_id || '-'}</div>
                <div><span className="font-bold text-gray-400">订单：</span>{copyTarget.order_id || '-'}</div>
                <div><span className="font-bold text-gray-400">售出：</span>{copyTarget.sold_at || '-'}</div>
              </div>
              <textarea
                readOnly
                value={copyTarget.content}
                rows={5}
                className="ios-input w-full rounded-xl border-none bg-gray-50 p-3 font-mono text-sm text-gray-700"
                onFocus={e => e.currentTarget.select()}
              />
              <div className="flex justify-end gap-2">
                <button onClick={copyContent}
                  className="ios-btn-primary inline-flex items-center gap-1.5 px-4 py-2.5 rounded-xl font-bold">
                  <Copy className="h-4 w-4" /> 复制内容
                </button>
              </div>
            </div>
          </div>
        </div>, document.body)}

      {/* 售出台账 */}
      {showSales && createPortal(
        <div className="modal-overlay-centered" onClick={() => setShowSales(false)}>
          <div className="modal-container" onClick={e => e.stopPropagation()} style={{ maxWidth: '880px' }}>
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 className="text-lg font-bold text-gray-900">售出台账</h3>
                <p className="text-xs text-gray-500 mt-1">
                  {card.name} · 共 {saleData.total} 条 · 卡种删除后记录仍保留
                </p>
              </div>
              <button onClick={() => setShowSales(false)} className="rounded-lg p-1 text-gray-400 hover:bg-gray-100">
                <X className="h-5 w-5" />
              </button>
            </div>
            <div className="space-y-4 p-6">
              <div className="flex flex-wrap items-end gap-3">
                <div>
                  <label className="mb-1 block text-xs font-bold text-gray-400">状态</label>
                  <select
                    value={saleStatus}
                    onChange={e => { setSaleStatus(e.target.value as CardSaleStatus | ''); setSalePage(1); }}
                    className="ios-input rounded-xl border-none bg-white py-2.5 px-3 text-sm shadow-sm"
                  >
                    <option value="">全部状态</option>
                    <option value="reserved">待核对</option>
                    <option value="sold">已售出</option>
                  </select>
                </div>
                <div className="min-w-[200px] flex-1">
                  <label className="mb-1 block text-xs font-bold text-gray-400">搜索</label>
                  <div className="relative">
                    <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
                    <input
                      value={saleSearch}
                      onChange={e => { setSaleSearch(e.target.value); setSalePage(1); }}
                      placeholder="订单号 / 买家 / 卡密内容"
                      className="ios-input w-full rounded-xl border-none bg-white py-2.5 pl-9 pr-3 text-sm shadow-sm"
                    />
                  </div>
                </div>
                <button
                  onClick={handleExportSales}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-gray-100 px-4 py-2.5 text-sm font-bold text-gray-700 hover:bg-gray-200"
                  title="按当前状态筛选导出 CSV"
                >
                  <FileDown className="h-4 w-4" /> 导出 CSV
                </button>
              </div>

              <div className="overflow-x-auto rounded-xl border border-gray-100">
                <table className="w-full border-collapse text-left">
                  <thead>
                    <tr className="bg-gray-50">
                      <th className="px-3 py-3 text-xs font-bold text-gray-400">卡密内容</th>
                      <th className="px-3 py-3 text-xs font-bold text-gray-400">状态</th>
                      <th className="px-3 py-3 text-xs font-bold text-gray-400">订单 / 买家</th>
                      <th className="px-3 py-3 text-xs font-bold text-gray-400">扣减时间</th>
                      <th className="px-3 py-3 text-xs font-bold text-gray-400">确认时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {salesBusy && (
                      <tr>
                        <td colSpan={5} className="px-3 py-8 text-center text-sm text-gray-400">
                          <Loader2 className="mx-auto mb-2 h-5 w-5 animate-spin" /> 加载中
                        </td>
                      </tr>
                    )}
                    {!salesBusy && saleData.records.length === 0 && (
                      <tr>
                        <td colSpan={5} className="px-3 py-8 text-center text-sm text-gray-400">暂无售出记录</td>
                      </tr>
                    )}
                    {!salesBusy && saleData.records.map(record => {
                      const meta = saleStatusMeta[record.status] || saleStatusMeta.reserved;
                      return (
                        <tr key={record.id} className="border-t border-gray-100 align-top">
                          <td className="max-w-[240px] px-3 py-3">
                            <div className="truncate font-mono text-xs text-gray-700" title={record.content}>{record.content}</div>
                            <div className="mt-1 text-[11px] text-gray-400">
                              卡种 ID {record.card_id}{record.run_id > 0 ? ` · 运行 #${record.run_id}` : ''}
                            </div>
                          </td>
                          <td className="px-3 py-3">
                            <span className={`inline-flex rounded-full px-2 py-0.5 text-[11px] font-bold ${meta.className}`}>{meta.label}</span>
                          </td>
                          <td className="px-3 py-3 text-xs text-gray-500">
                            <div>{record.order_id || '-'}</div>
                            <div className="mt-1 text-gray-400">{record.buyer_id || '-'}{record.cookie_id ? ` · ${record.cookie_id}` : ''}</div>
                          </td>
                          <td className="px-3 py-3 text-xs text-gray-500">{formatLocalDateTime(record.created_at * 1000)}</td>
                          <td className="px-3 py-3 text-xs text-gray-500">{record.sold_at > 0 ? formatLocalDateTime(record.sold_at * 1000) : '-'}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>

              <div className="flex items-center justify-end gap-2">
                <button onClick={() => setSalePage(p => Math.max(1, p - 1))} disabled={salePage <= 1 || salesBusy}
                  className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 disabled:opacity-40">
                  <ChevronLeft className="h-4 w-4" /> 上一页
                </button>
                <span className="text-xs font-bold text-gray-500">{salePage} / {saleTotalPages}</span>
                <button onClick={() => setSalePage(p => Math.min(saleTotalPages, p + 1))} disabled={salePage >= saleTotalPages || salesBusy}
                  className="inline-flex items-center gap-1 rounded-xl bg-gray-100 px-3 py-2 text-xs font-bold text-gray-600 disabled:opacity-40">
                  下一页 <ChevronRight className="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>
        </div>, document.body)}
    </div>
  );
};

export default CardItems;
