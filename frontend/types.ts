
// API Response Bases
export interface ApiResponse {
  success?: boolean;
  message?: string;
  msg?: string;
}

export interface PaginatedResponse<T> {
  success: boolean;
  data: T[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  trigger_counts?: Record<string, number>;
}

// Auth
export interface LoginResponse {
  success: boolean;
  token?: string;
  message?: string;
  user_id?: number;
  username?: string;
  is_admin?: boolean;
}

// Accounts
export interface AccountDetail {
  id: string;
  value?: string; // cookie value from backend
  cookie?: string; // alias for value
  enabled: boolean;
  auto_confirm: boolean;
  remark?: string;
  note?: string; // alias for remark
  pause_duration?: number;
  paused_until?: number;
  paused?: boolean;
  // 登录信息
  username?: string;
  login_password?: string;
  show_browser?: boolean;
  // Frontend helpers
  nickname?: string;
  avatar_url?: string;
  profile_error?: string;
  runtime_state?: 'starting' | 'connecting' | 'online' | 'reconnecting' | 'auth_expired' | 'verification_required' | 'error' | 'stopped' | 'disabled';
  runtime_message?: string;
  runtime_connected?: boolean;
  runtime_updated_at?: string;
  // AI设置
  ai_enabled?: boolean;
  max_discount_percent?: number;
  max_discount_amount?: number;
  max_bargain_rounds?: number;
  custom_prompts?: string;
	// 账号级计划任务
	auto_rate_enabled?: boolean;
	rate_content?: string;
	auto_polish_enabled?: boolean;
	polish_time?: string;
	// 小刀（砍价成团）自动免拼
	auto_freeshipping_enabled?: boolean;
	// 自动求小红花
	auto_red_flower_enabled?: boolean;
	last_red_flower_scan_at?: number;
	last_rate_scan_at?: number;
	last_polish_date?: string;
	last_polish_at?: number;
}

export interface AIProfile {
  id: number;
  cookie_id: string;
  name: string;
  enabled: boolean;
  use_system_api: boolean;
  has_api_key: boolean;
  base_url: string;
  model_name: string;
  custom_prompts: string;
  thinking_mode: 'enabled' | 'disabled';
  bargain_strategy_enabled: boolean;
  trigger_mode: 'all_text';
  max_discount_percent: number;
  max_discount_amount: number;
  max_bargain_rounds: number;
  item_ids: string[];
}

export interface AIForbiddenWord {
  id?: number;
  keyword: string;
  replacement: string;
  enabled: boolean;
  sort_order?: number;
}

export interface AIProfileInput {
  cookie_id: string;
  name: string;
  enabled: boolean;
  use_system_api: boolean;
  api_key?: string;
  base_url: string;
  model_name: string;
  custom_prompts: string;
  thinking_mode: 'enabled' | 'disabled';
  bargain_strategy_enabled: boolean;
  max_discount_percent: number;
  max_discount_amount: number;
  max_bargain_rounds: number;
  item_ids: string[];
}

export interface AccountTaskSettings {
	auto_freeshipping_enabled?: boolean;
	auto_red_flower_enabled?: boolean;
	last_red_flower_scan_at?: number;
	account_id: string;
	auto_rate_enabled: boolean;
	rate_content: string;
	auto_polish_enabled: boolean;
	polish_time: string;
	last_rate_scan_at?: number;
	last_polish_date?: string;
	last_polish_at?: number;
}

export interface AccountTaskSummary {
	task_type: 'auto_rate' | 'auto_polish';
	found: number;
	success: number;
	failed: number;
	skipped: number;
	message?: string;
}

export interface ChatSession {
	account_id: string;
	chat_id: string;
	buyer_id: string;
	buyer_name: string;
	buyer_avatar_url?: string;
	item_id?: string;
	item_title?: string;
	last_message: string;
	last_message_at: number;
	unread_count: number;
}

export interface ChatMessage {
	id: number;
	account_id: string;
	chat_id: string;
	message_key: string;
	direction: 'incoming' | 'outgoing';
	sender_id: string;
	sender_name: string;
	/** text/image/video are peer messages; system is an official platform notice or trade card. */
	message_type: 'text' | 'image' | 'video' | 'system';
	content: string;
	status: 'received' | 'sending' | 'sent' | 'failed';
	read_status?: number;
	read_at?: number;
	sent_at: number;
}

// Orders
export type OrderStatus = 
  | 'processing'      
  | 'pending_ship'    
  | 'shipped'         
  | 'completed'       
  | 'cancelled'       
  | 'refunding'
  | 'unknown';

export interface Order {
  id: string;
  order_id: string;
  cookie_id: string;
  item_id: string;
  item_title?: string;
  item_image?: string;
  item_price?: string;
  buyer_id: string;
  quantity: number;
  amount: string;
  status: OrderStatus;
  order_status?: OrderStatus;
  receiver_name?: string;
  receiver_phone?: string;
  receiver_address?: string;
  created_at?: string;
  updated_at?: string;
}

// Cards
// 卡密类型（卡种属性）：card=批量卡密（一次性消耗），text=固定文本，image=固定图片。
export type CardKind = 'card' | 'text' | 'image';

export interface Card {
  id: number;
  name: string;
  description?: string;
  enabled: boolean;
  delay_seconds?: number;
  // 多规格配置
  is_multi_spec?: boolean;
  spec_name?: string;
  spec_value?: string;
  kind: CardKind;
  item_count?: number;
  // 库存统计（卡种列表/详情带出）
  stock?: CardStock;
  created_at: string;
  updated_at: string;
}

// 卡密条目（逐条状态管理）
export type CardItemStatus = 'available' | 'used' | 'void';

export interface CardStock {
  card_id: number;
  total: number;
  available: number;
  used: number;
  void: number;
}

export interface CardItem {
  id: number;
  card_id: number;
  card_name?: string;
  content: string;
  status: CardItemStatus;
  buyer_id: string;
  buyer_name: string;
  order_id: string;
  spec_name: string;
  spec_value: string;
  quantity: number;
  sold_at: string;
  created_at: string;
  updated_at: string;
}

export interface CardItemPage {
  items: CardItem[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

// 卡密售出台账：预留时 reserved，确认发送后 sold；卡种删除后仍保留。
export type CardSaleStatus = 'reserved' | 'sold';

export interface CardSaleRecord {
  id: number;
  card_id: number;
  card_item_id: number;
  card_name: string;
  card_kind: string;
  content: string;
  status: CardSaleStatus;
  run_id: number;
  cookie_id: string;
  order_id: string;
  item_id: string;
  buyer_id: string;
  created_at: number;
  sold_at: number;
}

export interface CardSalePage {
  records: CardSaleRecord[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

// Items
export interface Item {
  id: string | number;
  cookie_id: string;
  item_id: string;
  item_title?: string;
  item_description?: string;
  item_price?: string;
  item_image?: string; // Inferred from common usage, though not explicitly in list model sometimes
  item_category?: string;
  item_detail?: string;
  is_multi_spec?: number | boolean;
  multi_quantity_delivery?: number | boolean;
  is_multi_qty_ship?: number | boolean;
  created_at?: string;
}

export type AutomationTriggerType = 'order_paid' | 'buyer_reviewed' | 'review_missing_timeout';
export type AutomationActionType = 'confirm_shipment' | 'send_card' | 'send_template' | 'send_text';

export interface DeliveryTemplateMessage {
  id?: number;
  sort_order?: number;
  content: string;
}

export interface DeliveryTemplate {
  id: number;
  name: string;
  enabled: boolean;
  messages: DeliveryTemplateMessage[];
  keys: string[];
  created_at?: string;
  updated_at?: string;
}

export interface DeliveryTemplateBinding {
  key: string;
  card_id: number;
  card_name?: string;
  delivery_count: number;
}

// Rules
export interface ShippingRule {
  id: string;
  name: string;
  trigger_type: AutomationTriggerType;
  item_keyword: string; // Legacy UI helper
  cookie_id?: string;
  item_id?: string;
  item_title?: string;
  card_group_id: number; // First send_card action card id
  card_group_name?: string; // UI helper
  priority: number;
  enabled: boolean;
  config_json?: string;
  actions: AutomationAction[];
  variants: ShippingVariant[];
}

export interface AutomationAction {
  id?: string;
  action_type: AutomationActionType;
  card_id?: number;
  card_name?: string;
  delivery_count?: number;
  message_template?: string;
  delay_seconds?: number;
  config_json?: string;
  enabled: boolean;
  sort_order?: number;
  delivery_template_id?: number;
  delivery_template_name?: string;
  template_bindings?: DeliveryTemplateBinding[];
}

export interface ShippingVariant {
  id?: string;
  spec_name: string;
  spec_value: string;
  delivery_mode?: 'card' | 'template';
  delivery_template_id?: number;
  template_bindings?: DeliveryTemplateBinding[];
  card_id: number;
  card_name?: string;
  card_type?: CardKind;
  delivery_count: number;
  enabled: boolean;
  delay_override?: boolean;
  delay_seconds?: number;
  config_json?: string;
}

export interface ReplyRule {
  id: string;
  keyword: string;
  /** 匹配表达式集合；每行一个，任意命中即回复。旧数据回退到 keyword。 */
  expressions?: string[];
  reply_content: string;
  match_type: 'contains' | 'regexp';
  enabled: boolean;
  item_id?: string;
  type?: 'text' | 'image';
  image_url?: string;
}

// Stats
export interface AdminStats {
  total_users: number;
  total_cookies: number;
  active_cookies: number;
  total_cards: number;
  total_keywords: number;
  total_orders: number;
}

export interface DashboardStats {
  total_cookies: number;
  active_cookies: number;
  total_cards: number;
  total_keywords: number;
  total_orders: number;
  available_card_stock: number;
}

export interface OrderAnalytics {
  revenue_stats: {
    total_amount: number;
    total_orders: number;
  };
  daily_stats: Array<{ date: string; amount: number; order_count: number }>;
  item_stats?: Array<{
    item_id: string;
    order_count: number;
    total_amount: number;
    avg_amount: number;
  }>;
}

// Settings
export interface SystemSettings {
  ai_model?: string;
  ai_api_key?: string;
  ai_api_url?: string;
  ai_base_url?: string;
  ai_thinking_mode?: 'enabled' | 'disabled' | string;
  default_reply?: string;
  registration_enabled?: boolean;
  smtp_server?: string;
  log_level?: 'debug' | 'info' | 'warn' | 'error' | string;
  log_format?: 'text' | 'json' | string;
  renewal_log_retention_days?: number;
  'captcha.remote_service_url'?: string;
  'captcha.remote_secret_key'?: string;
  'captcha.remote_pass_cookies'?: boolean | string;
  [key: string]: any;
}

export interface AIReplySettings {
  ai_enabled: boolean;
  model_name?: string;
  api_key?: string;
  base_url?: string;
  max_discount_percent: number;
  max_discount_amount?: number;
  max_bargain_rounds: number;
  custom_prompts: string;
}

// Default Reply
export interface DefaultReply {
  cookie_id: string;
  enabled: boolean;
  reply_content: string;
  reply_once: boolean;
  reply_image_url?: string;
}

// 商品首次回复规则：买家首次咨询指定商品时发送一次。
export interface ItemGreetingRule {
  id: number;
  cookie_id: string;
  name: string;
  enabled: boolean;
  reply_content: string;
  reply_image_url: string;
  item_ids: string[];
}

export interface ItemGreetingRuleInput {
  cookie_id: string;
  name: string;
  enabled: boolean;
  reply_content: string;
  reply_image_url: string;
  item_ids: string[];
}

// 通知渠道
export type NotificationChannelType = 'dingtalk' | 'feishu' | 'bark' | 'webhook' | 'wechat' | 'telegram' | 'email';
export type NotificationEventType =
  | 'account_offline'
  | 'account_recovered'
  | 'account_disabled'
  | 'security_verification'
  | 'token_renewal'
  | 'delivery_result'
  | 'human_handoff'
  | 'system_error';

export interface NotificationChannel {
  id: string;
  name: string;
  type: NotificationChannelType;
  config: Record<string, unknown>;
  event_types?: NotificationEventType[];
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}
