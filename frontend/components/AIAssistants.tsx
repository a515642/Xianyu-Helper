import React, { useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import { Bot, Loader2, Plus, Save, ShieldAlert, Trash2, X } from 'lucide-react';
import { AccountDetail, AIForbiddenWord, AIProfile, AIProfileInput, Item } from '../types';
import { createAIProfile, deleteAIProfile, fetchAIModels, getAIForbiddenWords, getAIProfiles, getAccountDetails, getItems, replaceAIForbiddenWords, updateAIProfile } from '../services/api';

const blankProfile = (cookieID: string): AIProfileInput => ({
  cookie_id: cookieID, name: '', enabled: true, use_system_api: true, base_url: '', model_name: '',
  custom_prompts: '', thinking_mode: 'disabled', bargain_strategy_enabled: false, max_discount_percent: 10, max_discount_amount: 100, max_bargain_rounds: 3, item_ids: [],
});

const AIAssistants: React.FC<{ isAdmin: boolean }> = ({ isAdmin }) => {
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [accountID, setAccountID] = useState('');
  const [items, setItems] = useState<Item[]>([]);
  const [profiles, setProfiles] = useState<AIProfile[]>([]);
  const [editing, setEditing] = useState<AIProfileInput | null>(null);
  const [editingID, setEditingID] = useState<number | null>(null);
  const [apiKey, setApiKey] = useState('');
  const [clearKey, setClearKey] = useState(false);
  const [forbiddenOpen, setForbiddenOpen] = useState(false);
  const [forbidden, setForbidden] = useState<AIForbiddenWord[]>([]);
  const [itemSearch, setItemSearch] = useState('');
  const [saving, setSaving] = useState(false);
  const [models, setModels] = useState<string[]>([]);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelError, setModelError] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = async (id = accountID) => {
    if (!id) return;
    setLoading(true); setError('');
    try {
      const [nextProfiles, nextItems] = await Promise.all([getAIProfiles(id), getItems(id)]);
      setProfiles(nextProfiles); setItems(nextItems);
    } catch (err) { setError(err instanceof Error ? err.message : 'AI 数据加载失败'); }
    finally { setLoading(false); }
  };

  useEffect(() => {
    getAccountDetails().then(next => {
      setAccounts(next); const first = next[0]?.id || ''; setAccountID(first); if (first) void load(first);
    }).catch(err => setError(err instanceof Error ? err.message : '账号加载失败'));
  }, []);

  const itemLabel = (id: string) => items.find(item => item.item_id === id)?.item_title || id;
  const assignedElsewhere = useMemo(() => {
    const map = new Map<string, string>();
    profiles.forEach(profile => (profile.item_ids || []).forEach(id => map.set(id, profile.name)));
    return map;
  }, [profiles]);
  const visibleItems = useMemo(() => {
    const query = itemSearch.trim().toLowerCase();
    if (!query) return items;
    return items.filter(item => `${item.item_title || ''} ${item.item_id}`.toLowerCase().includes(query));
  }, [items, itemSearch]);

  const openCreate = () => { setEditing(blankProfile(accountID)); setEditingID(null); setApiKey(''); setClearKey(false); setModels([]); setModelError(''); setItemSearch(''); };
  const openEdit = (profile: AIProfile) => {
    setEditing({ cookie_id: profile.cookie_id, name: profile.name, enabled: profile.enabled, use_system_api: profile.use_system_api, base_url: profile.base_url, model_name: profile.model_name, custom_prompts: profile.custom_prompts, thinking_mode: profile.thinking_mode || 'disabled', bargain_strategy_enabled: profile.bargain_strategy_enabled === true, max_discount_percent: profile.max_discount_percent, max_discount_amount: profile.max_discount_amount, max_bargain_rounds: profile.max_bargain_rounds, item_ids: [...(profile.item_ids || [])] });
    setEditingID(profile.id); setApiKey(''); setClearKey(false); setModels([]); setModelError(''); setItemSearch('');
  };
  const loadModels = async () => {
    if (!editing || editing.use_system_api || !editing.base_url.trim() || !apiKey.trim()) {
      setModelError('请先填写自定义 API 地址和 API Key'); return;
    }
    setModelsLoading(true); setModelError('');
    try { setModels(await fetchAIModels(editing.base_url.trim(), apiKey.trim())); }
    catch (err) { setModels([]); setModelError(err instanceof Error ? err.message : '读取模型失败'); }
    finally { setModelsLoading(false); }
  };

  const saveProfile = async () => {
    if (!editing) return;
    if (!editing.name.trim()) { setError('AI 名称不能为空'); return; }
    setSaving(true); setError('');
    try {
      const input = { ...editing, api_key: apiKey || undefined, clear_api_key: clearKey };
      if (editingID) await updateAIProfile(editingID, input); else await createAIProfile(input);
      setEditing(null); await load();
    } catch (err) { setError(err instanceof Error ? err.message : '保存 AI 助手失败'); }
    finally { setSaving(false); }
  };
  const removeProfile = async (id: number) => {
    if (!window.confirm('删除 AI 助手后会解除商品绑定，但不会删除商品和会话历史，确认继续？')) return;
    try { await deleteAIProfile(id); await load(); } catch (err) { setError(err instanceof Error ? err.message : '删除失败'); }
  };
  const openForbidden = async () => {
    try { setForbidden(await getAIForbiddenWords()); setForbiddenOpen(true); } catch (err) { setError(err instanceof Error ? err.message : '违禁词加载失败'); }
  };
  const saveForbidden = async () => {
    setSaving(true); try { await replaceAIForbiddenWords(forbidden); setForbiddenOpen(false); } catch (err) { setError(err instanceof Error ? err.message : '违禁词保存失败'); } finally { setSaving(false); }
  };

  return <div className="space-y-8 animate-fade-in">
    <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
      <div><h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">AI 助手</h2><p className="mt-2 font-medium text-gray-500">按闲鱼账号管理多个 AI，并将每个 AI 应用到多个商品。</p></div>
      <div className="flex gap-3">
        <select value={accountID} onChange={event => { setAccountID(event.target.value); void load(event.target.value); }} className="ios-input rounded-2xl px-4 py-3 font-bold">
          {accounts.map(account => <option key={account.id} value={account.id}>{account.nickname || account.remark || account.id}</option>)}
        </select>
        {isAdmin && <button type="button" onClick={openForbidden} className="flex items-center gap-2 rounded-2xl bg-amber-50 px-5 py-3 font-bold text-amber-700 hover:bg-amber-100"><ShieldAlert className="h-5 w-5" />违禁词</button>}
        <button type="button" onClick={openCreate} disabled={!accountID} className="ios-btn-primary flex items-center gap-2 rounded-2xl px-5 py-3 font-bold"><Plus className="h-5 w-5" />创建 AI</button>
      </div>
    </div>
    {error && <div className="rounded-2xl bg-red-50 px-5 py-4 font-bold text-red-700">{error}</div>}
    {loading ? <div className="flex justify-center py-20"><Loader2 className="h-8 w-8 animate-spin text-sky-500" /></div> : profiles.length === 0 ? <div className="ios-card rounded-2xl p-10 text-center text-gray-500">当前账号还没有 AI 助手，请先创建。</div> : <div className="grid gap-5 lg:grid-cols-2">{profiles.map(profile => <div key={profile.id} className="ios-card rounded-2xl p-6">
      <div className="flex items-start justify-between gap-4"><div><h3 className="flex items-center gap-2 text-xl font-extrabold text-gray-900"><Bot className="h-5 w-5 text-purple-500" />{profile.name}</h3><p className="mt-2 text-sm text-gray-500">{profile.use_system_api ? '继承系统 API 配置' : '使用自定义 API 配置'} · {profile.model_name || '系统默认模型'}</p></div><span className={`rounded-lg px-2.5 py-1 text-xs font-bold ${profile.enabled ? 'bg-emerald-100 text-emerald-700' : 'bg-gray-100 text-gray-500'}`}>{profile.enabled ? '已启用' : '已停用'}</span></div>
      <div className="mt-5 flex flex-wrap gap-2">{(profile.item_ids || []).map(id => <span key={id} className="rounded-lg bg-blue-50 px-2.5 py-1 text-xs font-bold text-blue-700">{itemLabel(id)}</span>)}{(profile.item_ids || []).length === 0 && <span className="text-sm text-gray-400">未绑定商品</span>}</div>
      <div className="mt-5 flex items-center justify-end gap-3"><button type="button" role="switch" aria-checked={profile.enabled} aria-label={`${profile.name}启用状态`} onClick={async () => { try { await updateAIProfile(profile.id, { cookie_id: profile.cookie_id, name: profile.name, enabled: !profile.enabled, use_system_api: profile.use_system_api, base_url: profile.base_url, model_name: profile.model_name, custom_prompts: profile.custom_prompts, thinking_mode: profile.thinking_mode, bargain_strategy_enabled: profile.bargain_strategy_enabled === true, max_discount_percent: profile.max_discount_percent, max_discount_amount: profile.max_discount_amount, max_bargain_rounds: profile.max_bargain_rounds, item_ids: profile.item_ids }); await load(); } catch (err) { setError(err instanceof Error ? err.message : '更新 AI 开关失败'); } }} className={`relative h-7 w-12 rounded-full transition-colors ${profile.enabled ? 'bg-brand' : 'bg-gray-300'}`}><span className={`absolute left-1 top-1 h-5 w-5 rounded-full bg-white shadow transition-transform ${profile.enabled ? 'translate-x-5' : 'translate-x-0'}`} /></button><button type="button" onClick={() => openEdit(profile)} className="rounded-xl bg-gray-100 px-4 py-2 font-bold text-gray-700 hover:bg-gray-200">编辑</button><button type="button" onClick={() => void removeProfile(profile.id)} className="rounded-xl bg-red-50 px-4 py-2 font-bold text-red-700 hover:bg-red-100"><Trash2 className="h-4 w-4" /></button></div>
    </div>)}</div>}

    {editing && createPortal(<div className="modal-overlay-centered"><div className="modal-container" style={{ maxWidth: '700px' }}><div className="modal-header flex items-center justify-between gap-4"><div className="min-w-0"><h3 className="text-2xl font-extrabold text-gray-900">{editingID ? '编辑 AI 助手' : '创建 AI 助手'}</h3><p className="mt-1 text-sm text-gray-500">AI 回复只会作用于已绑定商品的买家会话。</p></div><button type="button" aria-label="关闭 AI 助手弹窗" onClick={() => setEditing(null)} disabled={saving} className="shrink-0 rounded-xl p-2 text-gray-500 hover:bg-gray-100"><X /></button></div><div className="modal-body space-y-5">
      <input value={editing.name} onChange={e => setEditing({ ...editing, name: e.target.value })} placeholder="AI 名称" className="ios-input w-full rounded-xl px-4 py-3" />
      <label className="flex items-center gap-3 font-bold"><input type="checkbox" checked={editing.enabled} onChange={e => setEditing({ ...editing, enabled: e.target.checked })} />启用 AI 自动回复</label>
      <label className="flex items-center gap-3 font-bold"><input type="checkbox" checked={editing.use_system_api} onChange={e => setEditing({ ...editing, use_system_api: e.target.checked })} />使用系统 API 配置</label>
      {!editing.use_system_api && <div className="space-y-3"><div className="grid gap-3 md:grid-cols-2"><input type="password" value={apiKey} onChange={e => { setApiKey(e.target.value); setModels([]); }} placeholder="API Key（留空保持）" className="ios-input rounded-xl px-3 py-3" /><input value={editing.base_url} onChange={e => { setEditing({ ...editing, base_url: e.target.value }); setModels([]); }} placeholder="Base URL" className="ios-input rounded-xl px-3 py-3" /></div><div className="flex gap-2"><input value={editing.model_name} onChange={e => setEditing({ ...editing, model_name: e.target.value })} placeholder="模型名称（可手动输入）" className="ios-input min-w-0 flex-1 rounded-xl px-3 py-3" /><button type="button" onClick={() => void loadModels()} disabled={modelsLoading} className="rounded-xl bg-gray-100 px-4 py-3 font-bold">{modelsLoading ? '读取中...' : '读取模型'}</button></div>{models.length > 0 && <select value={editing.model_name} onChange={e => setEditing({ ...editing, model_name: e.target.value })} className="ios-input w-full rounded-xl px-3 py-3"><option value="">选择模型</option>{models.map(model => <option key={model} value={model}>{model}</option>)}</select>}{modelError && <p className="text-xs font-bold text-red-600">{modelError}</p>}<label className="text-sm font-bold text-gray-600"><input type="checkbox" checked={clearKey} onChange={e => setClearKey(e.target.checked)} /> 清除已保存 Key</label></div>}{!editing.use_system_api && <label className="flex items-center gap-3 font-bold"><input type="checkbox" checked={editing.thinking_mode === 'enabled'} onChange={e => setEditing({ ...editing, thinking_mode: e.target.checked ? 'enabled' : 'disabled' })} />启用思考模式 <span className="text-xs font-normal text-gray-500">可能增加响应时间和 Token 消耗</span></label>}
      <div><textarea value={editing.custom_prompts} onChange={e => setEditing({ ...editing, custom_prompts: e.target.value })} placeholder="自定义提示词，例如：你是{{item_title}}的客服，商品详情：{{item_description}}" className="ios-input h-32 w-full resize-none rounded-xl px-4 py-3" /><p className="mt-1 text-xs text-gray-500">可用变量：{'{{item_title}}'} 商品名称、{'{{item_price}}'} 商品价格、{'{{item_description}}'} 商品详情。</p></div><label className="flex items-center gap-3 rounded-xl bg-amber-50 p-4 font-bold text-amber-900"><input type="checkbox" checked={editing.bargain_strategy_enabled} onChange={e => setEditing({ ...editing, bargain_strategy_enabled: e.target.checked })} />启用砍价策略 <span className="text-xs font-normal text-amber-700">关闭时遇到砍价消息时没有调整价格的能力</span></label><div className="border-t border-gray-200 pt-5"><h4 className="mb-3 text-lg font-bold text-gray-900">砍价策略参数</h4><div className="grid gap-3 md:grid-cols-3"><label className="text-sm font-bold text-gray-700">最大折扣比例 (%)<input type="number" min="0" max="100" value={editing.max_discount_percent} onChange={e => setEditing({ ...editing, max_discount_percent: Number(e.target.value) || 0 })} className="ios-input mt-2 w-full rounded-xl px-3 py-3" /></label><label className="text-sm font-bold text-gray-700">最大折扣金额 (元)<input type="number" min="0" value={editing.max_discount_amount} onChange={e => setEditing({ ...editing, max_discount_amount: Number(e.target.value) || 0 })} className="ios-input mt-2 w-full rounded-xl px-3 py-3" /></label><label className="text-sm font-bold text-gray-700">最大砍价轮次<input type="number" min="1" max="10" value={editing.max_bargain_rounds} onChange={e => setEditing({ ...editing, max_bargain_rounds: Number(e.target.value) || 1 })} className="ios-input mt-2 w-full rounded-xl px-3 py-3" /></label></div></div>      <div><div className="mb-2 flex items-center justify-between gap-3"><span className="font-bold text-gray-700">绑定商品（可多选；一个商品只能绑定一个 AI）</span><span className="text-xs text-gray-500">已选 {editing.item_ids.length} 个</span></div><input value={itemSearch} onChange={e => setItemSearch(e.target.value)} placeholder="搜索商品名称或 ID" className="ios-input mb-2 w-full rounded-xl px-3 py-2" /><div className="mb-2 flex gap-2"><button type="button" onClick={() => setEditing({ ...editing, item_ids: Array.from(new Set([...editing.item_ids, ...visibleItems.map(item => item.item_id)])) })} className="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold">全选当前结果</button><button type="button" onClick={() => setEditing({ ...editing, item_ids: editing.item_ids.filter(id => !visibleItems.some(item => item.item_id === id)) })} className="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold">取消当前结果</button></div><div className="max-h-48 space-y-2 overflow-y-auto rounded-xl border border-gray-200 p-3">{visibleItems.map(item => { const selected = editing.item_ids.includes(item.item_id); const owner = assignedElsewhere.get(item.item_id); return <label key={item.item_id} className="flex items-center gap-3 rounded-lg p-2 hover:bg-gray-50"><input type="checkbox" checked={selected} onChange={e => setEditing({ ...editing, item_ids: e.target.checked ? [...editing.item_ids, item.item_id] : editing.item_ids.filter(id => id !== item.item_id) })} /><span className="truncate text-sm font-medium">{item.item_title || item.item_id}</span>{owner && !selected && <span className="ml-auto text-xs text-amber-600">已绑定：{owner}</span>}</label>})}{visibleItems.length === 0 && <p className="py-4 text-center text-sm text-gray-400">没有匹配商品</p>}</div></div>
    </div><div className="modal-footer"><div className="flex w-full justify-end gap-3"><button type="button" onClick={() => setEditing(null)} disabled={saving} className="rounded-xl bg-gray-100 px-5 py-3 font-bold">取消</button><button type="button" onClick={() => void saveProfile()} disabled={saving} className="ios-btn-primary flex items-center gap-2 rounded-xl px-5 py-3 font-bold">{saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}保存</button></div></div></div></div>, document.body)}

    {forbiddenOpen && createPortal(<div className="modal-overlay-centered"><div className="modal-container" style={{ maxWidth: '700px' }}><div className="modal-header flex items-center justify-between gap-4"><div className="min-w-0"><h3 className="text-2xl font-extrabold text-gray-900">全局违禁词</h3><p className="mt-1 text-sm text-gray-500">AI 回复发送前会按列表顺序进行字面替换。</p></div><button type="button" aria-label="关闭违禁词弹窗" onClick={() => setForbiddenOpen(false)} disabled={saving} className="shrink-0 rounded-xl p-2 text-gray-500 hover:bg-gray-100"><X /></button></div><div className="modal-body space-y-3">{forbidden.map((rule, index) => <div key={rule.id || index} className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2"><input value={rule.keyword} onChange={e => setForbidden(current => current.map((r,i) => i===index ? {...r,keyword:e.target.value} : r))} placeholder="违禁词" className="ios-input rounded-lg px-3 py-2" /><input value={rule.replacement} onChange={e => setForbidden(current => current.map((r,i) => i===index ? {...r,replacement:e.target.value} : r))} placeholder="替换词" className="ios-input rounded-lg px-3 py-2" /><input type="checkbox" checked={rule.enabled} onChange={e => setForbidden(current => current.map((r,i) => i===index ? {...r,enabled:e.target.checked} : r))} /><button type="button" onClick={() => setForbidden(current => current.filter((_,i) => i!==index))} className="text-red-600"><Trash2 className="h-4 w-4" /></button></div>)}<button type="button" onClick={() => setForbidden(current => [...current,{keyword:'',replacement:'',enabled:true}])} className="rounded-xl bg-gray-100 px-4 py-2 font-bold">+ 添加规则</button></div><div className="modal-footer"><div className="flex w-full justify-end gap-3"><button type="button" onClick={() => setForbiddenOpen(false)} disabled={saving} className="rounded-xl bg-gray-100 px-5 py-3 font-bold">取消</button><button type="button" onClick={() => void saveForbidden()} disabled={saving} className="ios-btn-primary rounded-xl px-5 py-3 font-bold">保存违禁词</button></div></div></div></div>, document.body)}
  </div>;
};

export default AIAssistants;
