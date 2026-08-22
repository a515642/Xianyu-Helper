import React, { useEffect, useState } from 'react';
import { Plus, Save, Trash2, ChevronUp, ChevronDown, X } from 'lucide-react';
import { DeliveryTemplate } from '../types';
import { createDeliveryTemplate, deleteDeliveryTemplate, getDeliveryTemplates, updateDeliveryTemplate } from '../services/api';

const emptyTemplate = (): DeliveryTemplate => ({ id: 0, name: '', enabled: true, keys: [], messages: [{ content: '' }] });

const DeliveryTemplates: React.FC = () => {
  const [templates, setTemplates] = useState<DeliveryTemplate[]>([]);
  const [editing, setEditing] = useState<DeliveryTemplate | null>(null);
  const [loading, setLoading] = useState(false);
  const load = async () => { setLoading(true); try { setTemplates(await getDeliveryTemplates()); } finally { setLoading(false); } };
  useEffect(() => { void load(); }, []);
  const keys = editing?.messages.flatMap(message => {
    const found: string[] = [];
    const re = /\{\{delivery\.cards\.([A-Za-z0-9_-]+)\}\}/g;
    let match: RegExpExecArray | null;
    while ((match = re.exec(message.content)) !== null) if (!found.includes(match[1])) found.push(match[1]);
    return found;
  }).filter((key, index, all) => all.indexOf(key) === index) || [];
  const save = async () => {
    if (!editing) return;
    const input = { name: editing.name, enabled: editing.enabled, messages: editing.messages.map(message => ({ content: message.content })) };
    try { if (editing.id) await updateDeliveryTemplate(editing.id, input); else await createDeliveryTemplate(input); setEditing(null); await load(); alert('保存成功'); }
    catch (error) { alert('保存失败：' + (error as Error).message); }
  };
  const remove = async (template: DeliveryTemplate) => {
    if (!confirm(`确定删除模板“${template.name}”吗？`)) return;
    try { await deleteDeliveryTemplate(template.id); await load(); } catch (error) { alert('删除失败：' + (error as Error).message); }
  };
  const move = (index: number, delta: number) => {
    if (!editing) return;
    const target = index + delta; if (target < 0 || target >= editing.messages.length) return;
    const messages = [...editing.messages]; [messages[index], messages[target]] = [messages[target], messages[index]]; setEditing({ ...editing, messages });
  };
  return <div className="min-w-0 space-y-6">
    <div className="flex items-end justify-between gap-4"><div><h2 className="text-4xl font-extrabold text-gray-900">发货模板</h2><p className="mt-2 text-gray-500">维护有序消息列表，模板不绑定具体卡密组。</p></div><button onClick={() => setEditing(emptyTemplate())} className="ios-btn-primary rounded-2xl px-5 py-3 font-bold flex items-center gap-2"><Plus className="h-4 w-4" />新建模板</button></div>
    {loading ? <div className="rounded-xl bg-white p-12 text-center text-gray-400">正在加载</div> : <div className="grid gap-3">{templates.map(template => <div key={template.id} className="rounded-2xl border border-gray-100 bg-white p-5 shadow-sm flex items-center justify-between gap-4"><div><div className="font-black text-gray-900">{template.name}</div><div className="mt-1 text-sm text-gray-500">{template.messages.length} 条消息 · {template.keys.length} 个卡密引用</div></div><div className="flex gap-2"><button onClick={() => setEditing({ ...template, messages: template.messages.map(message => ({ ...message })) })} className="rounded-xl bg-gray-100 px-3 py-2 text-sm font-bold">编辑</button><button onClick={() => void remove(template)} className="rounded-xl p-2 text-red-500 hover:bg-red-50"><Trash2 className="h-4 w-4" /></button></div></div>)}</div>}
    {editing && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"><div className="max-h-[90vh] w-full max-w-3xl overflow-y-auto rounded-3xl bg-white shadow-xl"><div className="flex items-center justify-between border-b p-5"><h3 className="text-xl font-black">{editing.id ? '编辑发货模板' : '新建发货模板'}</h3><button onClick={() => setEditing(null)}><X /></button></div><div className="space-y-5 p-5"><input value={editing.name} onChange={event => setEditing({ ...editing, name: event.target.value })} placeholder="模板名称" className="ios-input w-full rounded-xl px-4 py-3" /><label className="flex items-center gap-2 text-sm font-bold"><input type="checkbox" checked={editing.enabled} onChange={event => setEditing({ ...editing, enabled: event.target.checked })} />启用模板</label><div className="space-y-3">{editing.messages.map((message, index) => <div key={index} className="flex gap-2"><textarea value={message.content} onChange={event => { const messages = [...editing.messages]; messages[index] = { ...messages[index], content: event.target.value }; setEditing({ ...editing, messages }); }} className="ios-input min-h-24 flex-1 resize-y rounded-xl px-4 py-3" placeholder={`第 ${index + 1} 条独立消息`} /><div className="flex flex-col gap-1"><button onClick={() => move(index, -1)} disabled={index === 0}><ChevronUp className="h-4 w-4" /></button><button onClick={() => move(index, 1)} disabled={index === editing.messages.length - 1}><ChevronDown className="h-4 w-4" /></button><button onClick={() => setEditing({ ...editing, messages: editing.messages.filter((_, i) => i !== index) })} disabled={editing.messages.length === 1}><Trash2 className="h-4 w-4 text-red-500" /></button></div></div>)}</div><button onClick={() => setEditing({ ...editing, messages: [...editing.messages, { content: '' }] })} className="rounded-xl bg-gray-100 px-4 py-2 text-sm font-bold">添加消息</button><div className="rounded-xl bg-blue-50 p-4 text-sm text-blue-800">卡密变量格式：<code>{'{{delivery.cards.<key>}' + '}'}</code>。当前检测到：{keys.length ? keys.join('、') : '暂无'}</div></div><div className="flex gap-3 border-t p-5"><button onClick={() => setEditing(null)} className="flex-1 rounded-xl bg-gray-100 py-3 font-bold">取消</button><button onClick={() => void save()} className="ios-btn-primary flex-1 rounded-xl py-3 font-bold flex justify-center gap-2"><Save className="h-4 w-4" />保存</button></div></div></div>}
  </div>;
};
export default DeliveryTemplates;
