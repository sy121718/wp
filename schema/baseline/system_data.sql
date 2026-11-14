-- 初始化基线：系统数据（由 scripts/baseline-generate.sh 生成，请勿手改）。
-- 只含迁移产生的初始数据，不含用户、订单、密钥、自定义译文等业务数据。
-- 生成时间：2026-11-15T06:37:23+08:00

--
-- PostgreSQL database dump
--



SET statement_timeout = 0;
SET lock_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET search_path = public, ext_shared;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Data for Name: admin_side_effect_tasks; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.admin_side_effect_tasks (id, kind, payload, create_time, claimed_time, processed_time, attempts, last_error) FROM stdin;
\.


--
-- Data for Name: ai_tool_idempotency; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.ai_tool_idempotency (id, tool_name, idem_key, result_text, result_data, create_time, actor) FROM stdin;
\.


--
-- Data for Name: projects; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.projects (id, name, settings, create_time, update_time, analytics_retention_days, masterdata_retention_days) FROM stdin;
\.


--
-- Data for Name: blocks; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.blocks (project_id, name, kind, document, create_time, update_time, category, reuse_mode, id, version, preset_key, preset_version) FROM stdin;
\.


--
-- Data for Name: blueprints; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.blueprints (id, name, kind, draft_document, draft_version, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: blueprint_versions; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.blueprint_versions (id, blueprint_id, version, document, create_time) FROM stdin;
\.


--
-- Data for Name: build_job_seq; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.build_job_seq (project_id, kind, next_version, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: build_jobs; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.build_jobs (source_type, source_id, draft_version, build_input_hash, status, artifact_id, error_message, create_time, started_at, completed_at, id, project_id, lang, intent, lease_token, lease_expires_time, attempt, input_version, context_key) FROM stdin;
\.


--
-- Data for Name: comments; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.comments (id, project_id, entity_type, entity_id, user_id, parent_id, body, status, author_ip_hash, reviewed_by, reviewed_at, create_time, update_time, deleted_at) FROM stdin;
\.


--
-- Data for Name: content_objects; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.content_objects (content_hash, provider, object_key, byte_size, create_time, deleted_at) FROM stdin;
\.


--
-- Data for Name: content_templates; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.content_templates (id, project_id, name, entity_type, draft_document, draft_version, current_version_id, create_time, update_time, is_default, template_role) FROM stdin;
\.


--
-- Data for Name: content_template_component_pins; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.content_template_component_pins (template_id, component_id, pinned_version_id) FROM stdin;
\.


--
-- Data for Name: content_template_versions; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.content_template_versions (id, template_id, version, document, source_hash, created_by, create_time) FROM stdin;
\.


--
-- Data for Name: contents; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.contents (id, entity_type, slug, revision, data, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: coupon_redemptions; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.coupon_redemptions (id, coupon_id, project_id, code, order_id, order_no, discount_amount, user_id, create_time) FROM stdin;
\.


--
-- Data for Name: coupons; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.coupons (id, project_id, code, name, discount_type, discount_value, min_subtotal, max_uses, used_count, per_user_limit, starts_at, ends_at, status, remark, create_by, update_by, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: currency_rates; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.currency_rates (id, base_code, quote_code, rate, rate_at, source, update_time) FROM stdin;
\.


--
-- Data for Name: presentation_instances; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.presentation_instances (id, project_id, entity_type, entity_id, url_path, template_id, current_snapshot_id, staged_snapshot_id, staged_artifact_id, active_artifact_id, stale, deleted_at, published_at, create_time, update_time, instance_role, override_document, render_mode, input_version) FROM stdin;
\.


--
-- Data for Name: document_snapshots; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.document_snapshots (id, presentation_instance_id, source_template_version_id, source_entity_revision_id, document, create_time) FROM stdin;
\.


--
-- Data for Name: inventory_change_reasons; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_change_reasons (project_id, code, name, direction, is_builtin, status, sort, create_time, update_time, id) FROM stdin;
\.


--
-- Data for Name: inventory_sources; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_sources (id, project_id, code, name, type, related_party, settle_price, status, config, sort, metadata, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: inventory_warehouses; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_warehouses (id, project_id, code, name, status, is_default, sort, metadata, create_time, update_time, type, config, deduct_priority) FROM stdin;
\.


--
-- Data for Name: inventory_purchase_orders; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_purchase_orders (id, project_id, code, source_id, warehouse_id, status, ordered_at, expected_at, remark, operator_id, metadata, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: product_brands; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_brands (id, project_id, name, slug, logo, description, seo_title, seo_description, sort, metadata, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: product_categories; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_categories (id, project_id, parent_id, name, slug, description, image, seo_title, seo_description, sort, metadata, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: products; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.products (id, project_id, name, subtitle, description, slug, status, sort, unit, weight, seo_title, seo_description, images, category_ids, tag_ids, related_ids, brand_id, default_price, default_compare_price, default_cost_price, default_image, metadata, create_time, update_time, attribute_ids, primary_category_id, published_at, images_alt, sku_code) FROM stdin;
\.


--
-- Data for Name: product_variants; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_variants (id, product_id, sku_code, barcode, price, compare_price, cost_price, image, option_values, enabled, sort, metadata, create_time, update_time, warehouse_skus) FROM stdin;
\.


--
-- Data for Name: inventory_purchase_order_lines; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_purchase_order_lines (id, order_id, project_id, product_id, variant_id, sku_code, quantity, received_quantity, unit_price, sort, remark, metadata, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: inventory_purchase_receipts; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_purchase_receipts (id, project_id, code, kind, order_id, source_id, warehouse_id, request_id, status, movement_batch_id, remark, operator_id, received_at, metadata, create_time) FROM stdin;
\.


--
-- Data for Name: inventory_purchase_receipt_items; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_purchase_receipt_items (id, receipt_id, project_id, line_id, product_id, variant_id, sku_code, quantity, unit_price, cost_updated, cost_error, create_time) FROM stdin;
\.


--
-- Data for Name: inventory_stock_movements_default; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_stock_movements_default (id, project_id, warehouse_id, product_id, variant_id, sku_code, direction, quantity, delta, quantity_before, quantity_after, reason_id, reason_code, parent_variant_id, source_type, source_ref, remark, operator_id, batch_id, create_time, unit_cost, stock_id, stock_name, stock_image_url) FROM stdin;
\.


--
-- Data for Name: inventory_stocks; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_stocks (id, project_id, warehouse_id, product_id, variant_id, sku_code, quantity, metadata, create_time, update_time, cost_price, external_sku, track_quantity, reserved_quantity, name, image_url, barcode, freight_price, cost_currency, cost_base_currency, cost_fx_rate, status, remark) FROM stdin;
\.


--
-- Data for Name: inventory_transfers; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_transfers (id, project_id, transfer_no, from_warehouse_id, to_warehouse_id, status, remark, create_by, shipped_by, received_by, shipped_at, received_at, request_id, create_time, update_time, requested_by, requested_at, approved_by, approved_at, approved_name, rejected_by, rejected_at, rejected_name, rejected_reason) FROM stdin;
\.


--
-- Data for Name: inventory_transfer_attachments; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_transfer_attachments (id, transfer_id, attachment_id, usage_type, sort, create_time) FROM stdin;
\.


--
-- Data for Name: inventory_transfer_items; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.inventory_transfer_items (id, transfer_id, stock_id, variant_id, sku_code, quantity) FROM stdin;
\.


--
-- Data for Name: mail_accounts; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_accounts (id, name, purpose, is_default, from_name, from_email, reply_to, provider, host, port, username, password_cipher, encryption, rate_per_hour, status, last_check_at, last_check_error, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: mail_automation_node_logs; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_automation_node_logs (id, run_id, node_key, node_type, status, detail, create_time) FROM stdin;
\.


--
-- Data for Name: mail_automation_runs; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_automation_runs (id, automation_id, automation_version, contact_id, status, current_node, next_run_at, error_message, trigger_event, started_at, finished_at, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: mail_automations; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_automations (id, name, description, trigger_type, trigger_params, definition, status, version, create_by, create_time, update_time, trigger_frequency, exit_on_reply) FROM stdin;
\.


--
-- Data for Name: mail_campaign_events; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_campaign_events (id, campaign_id, contact_id, event_type, url, ip, user_agent, create_time) FROM stdin;
\.


--
-- Data for Name: mail_campaigns; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_campaigns (id, name, account_id, template_id, target_tags, subject, variables, status, scheduled_at, started_at, finished_at, total_count, sent_count, failed_count, create_by, create_time, update_time, open_count, click_count) FROM stdin;
\.


--
-- Data for Name: mail_contacts; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_contacts (id, email, name, user_id, source, status, subscribed_at, consent_source, tags, attributes, last_activity_at, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: mail_logs; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_logs (id, account_id, template_key, to_email, subject, status, provider, provider_msg_id, error_kind, error_message, retry_count, sent_at, create_time, campaign_id, contact_id) FROM stdin;
\.


--
-- Data for Name: mail_suppressions; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_suppressions (id, email, reason, source, note, create_time) FROM stdin;
\.


--
-- Data for Name: mail_templates; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.mail_templates (id, template_key, locale, name, subject, body_html, body_text, variables, status, create_time, update_time, body_json, body_compiled_html, body_compiled_text) FROM stdin;
\.


--
-- Data for Name: master_data_changes_default; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.master_data_changes_default (id, project_id, entity_type, entity_id, entity_label, action, field, old_value, new_value, origin, operator_id, create_time) FROM stdin;
\.


--
-- Data for Name: membership_assignments; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.membership_assignments (id, project_id, user_id, tier_id, source, assigned_at, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: membership_entitlements; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.membership_entitlements (id, tier_id, kind, value_int, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: membership_tiers; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.membership_tiers (id, project_id, name, sort_order, threshold_amount, is_default, remark, create_time, update_time, deleted_at) FROM stdin;
\.


--
-- Data for Name: navigations; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.navigations (id, project_id, title, path, kind, parent_id, sort_order, create_time, update_time, source_type, source_id, target, panel_block_id, panel_width) FROM stdin;
\.


--
-- Data for Name: order_carriers; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.order_carriers (id, project_id, code, name, tracking_url_template, status, sort, metadata, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: order_items; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.order_items (id, order_id, product_id, variant_id, product_name, variant_label, sku, unit_price, quantity, line_subtotal, line_discount, line_tax, line_total, cost_price, create_time, warehouse_id) FROM stdin;
\.


--
-- Data for Name: order_return_items; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.order_return_items (id, return_id, order_item_id, product_id, variant_id, product_name, variant_label, sku, unit_price, quantity, received_quantity, refund_amount, create_time) FROM stdin;
\.


--
-- Data for Name: order_returns; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.order_returns (id, project_id, order_id, order_no, return_no, status, reason, refund_amount, user_id, customer_email, customer_name, admin_note, reviewer_id, reviewer_name, reviewed_at, received_at, refunded_at, transaction_id, request_id, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: order_shipments; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.order_shipments (id, project_id, order_id, shipment_no, carrier_id, carrier_name, tracking_no, shipped_at, operator_id, operator_name, remark, request_id, create_time) FROM stdin;
\.


--
-- Data for Name: order_shipment_items; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.order_shipment_items (id, shipment_id, order_item_id, warehouse_id, stock_id, quantity) FROM stdin;
\.


--
-- Data for Name: order_status_logs; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.order_status_logs (id, order_id, from_status, to_status, operator_type, operator_id, operator_name, remark, create_time) FROM stdin;
\.


--
-- Data for Name: orders; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.orders (id, project_id, order_no, status, user_id, customer_email, customer_name, customer_phone, currency, subtotal, discount_total, shipping_total, tax_total, total, ship_name, ship_phone, ship_province, ship_city, ship_district, ship_address, ship_zip, bill_name, bill_phone, bill_province, bill_city, bill_district, bill_address, bill_zip, payment_method, payment_method_title, transaction_id, paid_at, completed_at, created_via, ip_address, user_agent, attribution, admin_note, request_id, remark, cancel_reason, create_by, create_time, update_time, membership_discount_total, ship_country, bill_country, carrier_id, carrier_name, tracking_no, shipped_at, fx_rate, fx_base_code, fulfillment_status) FROM stdin;
\.


--
-- Data for Name: themes; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.themes (id, project_id, name, settings, is_active, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: pages; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.pages (id, project_id, kind, content_target_type, content_target_id, source_blueprint_version_id, draft_path, active_path, draft_document, draft_version, staged_artifact_id, active_artifact_id, stale, deleted_at, published_at, create_time, update_time, theme_id, rebuild_failed_at, rebuild_failed_stage, excluded_langs, input_version) FROM stdin;
\.


--
-- Data for Name: page_artifacts; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_artifacts (id, page_id, version, source_document, page_document_schema_version, source_hash, build_input_manifest, build_input_hash, artifact_provider, artifact_key, artifact_hash, compiler_version, registry_version, manifest, payload_state, payload_deleted_at, note, created_by, create_time, lang) FROM stdin;
\.


--
-- Data for Name: page_artifact_objects; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_artifact_objects (artifact_id, content_hash) FROM stdin;
\.


--
-- Data for Name: page_component_pins; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_component_pins (page_id, component_id, pinned_version_id) FROM stdin;
\.


--
-- Data for Name: page_dependencies; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_dependencies (page_id, artifact_id, dependency_kind, dependency_key, revision, last_checked) FROM stdin;
\.


--
-- Data for Name: page_publication_plans; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_publication_plans (page_id, lang, plan, plan_hash, draft_version, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: page_publications; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id) FROM stdin;
\.


--
-- Data for Name: page_revisions; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_revisions (id, page_id, version, draft_path, draft_document, source_hash, create_time) FROM stdin;
\.


--
-- Data for Name: page_routes; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_routes (project_id, path, page_id, presentation_id, route_kind, artifact_id, update_time) FROM stdin;
\.


--
-- Data for Name: page_stagings; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_stagings (page_id, lang, artifact_id, artifact_hash, draft_version, update_time) FROM stdin;
\.


--
-- Data for Name: page_views_daily; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_views_daily (project_id, day, path, views, visitors, scope, rolled_at) FROM stdin;
\.


--
-- Data for Name: page_views_default; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.page_views_default (id, project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at) FROM stdin;
\.


--
-- Data for Name: plugin_registry; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.plugin_registry (plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time) FROM stdin;
\.


--
-- Data for Name: presentation_artifacts; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.presentation_artifacts (id, presentation_instance_id, snapshot_id, version, source_hash, build_input_hash, artifact_provider, artifact_key, artifact_hash, compiler_version, registry_version, manifest, payload_state, payload_deleted_at, note, created_by, create_time, lang) FROM stdin;
\.


--
-- Data for Name: presentation_dependencies; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.presentation_dependencies (presentation_id, artifact_id, dependency_kind, dependency_key, revision, last_checked) FROM stdin;
\.


--
-- Data for Name: presentation_publications; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.presentation_publications (presentation_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id) FROM stdin;
\.


--
-- Data for Name: presentation_publish_attempts; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.presentation_publish_attempts (id, project_id, instance_id, entity_type, entity_id, trigger, outcome, error_text, langs, attempts, artifact_version, artifact_hash, operator_id, operator_name, duration_ms, create_time) FROM stdin;
\.


--
-- Data for Name: product_attributes; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_attributes (id, project_id, key, name, is_variation, sort, "values", metadata, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: product_outbox_events; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_outbox_events (id, project_id, entity_type, entity_id, entity_revision, dependency_kind, dependency_key, create_time, claimed_time, processed_time, attempts, last_error) FROM stdin;
\.


--
-- Data for Name: product_price_adjustments; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_price_adjustments (id, project_id, rule_type, rule_params, rounding, scope, target_id, filter, variant_count, changed_count, note, operator_id, create_time) FROM stdin;
\.


--
-- Data for Name: product_price_adjustment_items; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_price_adjustment_items (id, adjustment_id, product_id, variant_id, sku_code, old_price, new_price, create_time) FROM stdin;
\.


--
-- Data for Name: product_ratings; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_ratings (id, project_id, product_id, score, source, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: product_tags; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.product_tags (id, project_id, name, slug, kind, rule_type, rule_params, sort, metadata, create_time, update_time, recalc_at) FROM stdin;
\.


--
-- Data for Name: project_locales; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: publication_receipts; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.publication_receipts (source_type, source_id, action, path, from_artifact_id, to_artifact_id, receipt_state, receipt_data, create_time, completed_at, id) FROM stdin;
\.


--
-- Data for Name: sys_admin; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_admin (id, dept_id, username, password, name, avatar, email, phone, status, is_admin, login_failure_count, locked_until_time, metadata, last_failure_time, register_ip, register_location, last_login_ip, last_login_location, last_login_isp, last_login_time, create_by, create_time, update_by, update_time, remark) FROM stdin;
\.


--
-- Data for Name: sys_area; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_area (code, kind, level, parent_code, name_zh, name_en, iso_code, alpha3, numeric_code, dial_code, default_currency, default_lang, source, enabled, sort_order, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: sys_file_category; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_file_category (id, category_name, category_code, parent_id, sort_order, icon, status, create_by, update_by, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: sys_attachment; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_attachment (id, category_id, file_name, file_path, file_size, file_type, mime_type, storage_type, storage_path, url, md5, extra_info, status, create_by, update_by, create_time, update_time, generation) FROM stdin;
\.


--
-- Data for Name: sys_casbin_rule; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_casbin_rule (id, ptype, v0, v1, v2, v3, v4, v5) FROM stdin;
\.


--
-- Data for Name: sys_config; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_config (id, group_key, group_name, config_data, remark, status, version, create_by, create_time, update_by, update_time) FROM stdin;
\.


--
-- Data for Name: sys_dept; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_dept (id, parent_id, ancestors, dept_name, dept_code, leader_id, sort_order, status, remark, create_by, create_time, update_by, update_time) FROM stdin;
\.


--
-- Data for Name: sys_dict; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_dict (type, code, url_code, iso639_1, iso639_2, ui_available, symbol, minor_unit, numeric_code, enabled, sort_order, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: sys_i18n; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_i18n (id, item_key, lang, item_value, http_code, status, create_time, update_time, category, remark) FROM stdin;
1	trSavedMarkFail	zh-CN	已保存 {n} 条译文，但未能标记页面待重建 —— 请手动重新发布一次，否则前台仍是旧译文。	200	1	2026-11-15 06:37:22.7095+08	2026-11-15 06:37:22.7095+08	admin	product translation saved but stale mark failed
2	trSavedMarkFail	en-US	Saved {n} translation(s), but failed to mark pages for rebuild — please republish manually, otherwise the site still shows the old text.	200	1	2026-11-15 06:37:22.7095+08	2026-11-15 06:37:22.7095+08	admin	product translation saved but stale mark failed
\.


--
-- Data for Name: sys_i18n_revision; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_i18n_revision (id, revision, update_time) FROM stdin;
1	0	2026-11-15 06:37:21.804+08
\.


--
-- Data for Name: sys_media_variant; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_media_variant (id, attachment_id, variant_type, file_path, width, height, file_size, mime_type, status, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: sys_menu_permission; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_menu_permission (id, menu_id, permission_code, create_time, update_time) FROM stdin;
1	1	i18n:manage	2026-11-15 06:37:22.653621+08	2026-11-15 06:37:22.653621+08
2	3	inventory:warehouse_list	2026-11-15 06:37:22.653621+08	2026-11-15 06:37:22.653621+08
3	4	inventory:reason_list	2026-11-15 06:37:22.653621+08	2026-11-15 06:37:22.653621+08
\.


--
-- Data for Name: sys_menus; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_menus (id, permission_code, title, parent_id, type, path, component, external_url, icon, status, is_hidden, is_public, is_system, sort_order, remark, create_by, create_time, update_by, update_time, deleted_at, title_key) FROM stdin;
1	i18n:manage	文案词条	0	2	/i18n	view.i18n			1	0	0	1	17	\N	0	2026-11-15 06:37:22.382736+08	0	2026-11-15 06:37:22.536454+08	\N	\N
2	\N	库存	0	1				warehouse	1	0	0	1	4	库存模块一级目录：库存流水 / 仓库 / 货源 / 采购 / 变动原因	0	2026-11-15 06:37:22.536454+08	0	2026-11-15 06:37:22.538678+08	\N	\N
3	inventory:warehouse_list	仓库管理	2	2	/admin/inventory/warehouses				1	0	0	1	2	仓库实体：短码 / 默认仓 / 状态	0	2026-11-15 06:37:22.536454+08	0	2026-11-15 06:37:22.538678+08	\N	\N
4	inventory:reason_list	变动原因字典	2	2	/admin/inventory/reasons				1	0	0	1	5	库存变动原因的取值集合（内置 + 自定义）	0	2026-11-15 06:37:22.536454+08	0	2026-11-15 06:37:22.538678+08	\N	\N
\.


--
-- Data for Name: sys_migration_marker; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_migration_marker (version, component, note, completed_at, completed_by) FROM stdin;
597-block-bulk-delete-grant-copy	permission	把历史 block:delete 主体的删除能力一次性复制到 block:bulk_delete（不随后续授权变动重放；源按 v3 识别，含 canonical 与 bulk 两条历史路径）	2026-11-15 06:37:22.704329+08	0
\.


--
-- Data for Name: sys_permission; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_permission (id, permission_code, permission_name, module, api_path, api_method, status, remark, create_by, create_time, update_by, update_time) FROM stdin;
1	media:download_batch	批量下载媒体	media	/api/media/download/batch	GET	1	\N	0	2026-11-15 06:37:21.787+08	0	2026-11-15 06:37:21.787+08
2	media:download	下载媒体资源包	media	/api/media/download	GET	1	\N	0	2026-11-15 06:37:21.787+08	0	2026-11-15 06:37:21.787+08
3	media:variants_generate	重新生成媒体变体	media	/api/media/variants/generate	POST	1	\N	0	2026-11-15 06:37:21.787+08	0	2026-11-15 06:37:21.787+08
4	i18n:manage	文案词条管理	i18n	/api/i18n/save	POST	1	\N	0	2026-11-15 06:37:22.382736+08	0	2026-11-15 06:37:22.382736+08
5	seo:audit	产物 SEO 体检	seo	/api/seo/audit	POST	1	\N	0	2026-11-15 06:37:22.384641+08	0	2026-11-15 06:37:22.384641+08
6	i18n:view	文案词条查看	i18n	/api/i18n/list	GET	1	\N	0	2026-11-15 06:37:22.592436+08	0	2026-11-15 06:37:22.592436+08
\.


--
-- Data for Name: sys_role; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_role (id, role_code, role_name, status, is_system, sort_order, remark, create_by, create_time, update_by, update_time) FROM stdin;
\.


--
-- Data for Name: sys_rule; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_rule (id, rule_name, domain, config, status, remark, create_by, create_time, update_by, update_time) FROM stdin;
\.


--
-- Data for Name: sys_rule_assignment; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_rule_assignment (id, rule_id, target_type, target_id, target_scope, create_by, create_time) FROM stdin;
\.


--
-- Data for Name: sys_translation; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.sys_translation (source_hash, context, lang, source_text, target_text, engine, update_time, project_id) FROM stdin;
\.


--
-- Data for Name: user_preferences; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.user_preferences (id, user_id, theme, locale, timezone, page_size, email_notify, sms_notify, profile_visibility, show_online, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: user_profiles; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.user_profiles (id, user_id, first_name, last_name, gender, birthday, bio, website, locale, timezone, country, province, city, address, postcode, phone, company, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: users; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.users (id, username, password, email, email_verified_at, status, nickname, display_name, avatar, activation_key, activation_expires_at, login_failure_count, locked_until_time, last_failure_time, register_ip, register_location, last_login_ip, last_login_location, registered_at, last_login_time, last_active_at, metadata, create_by, create_time, update_time, deleted_at) FROM stdin;
\.


--
-- Data for Name: webhook_endpoints; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.webhook_endpoints (id, event_type, target_url, secret_cipher, description, status, create_time, update_time) FROM stdin;
\.


--
-- Data for Name: webhook_deliveries; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.webhook_deliveries (id, endpoint_id, event_type, payload, status, attempts, response_status, last_error, create_time, update_time) FROM stdin;
\.


--
-- Name: admin_side_effect_tasks_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.admin_side_effect_tasks_id_seq', 1, false);


--
-- Name: ai_tool_idempotency_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.ai_tool_idempotency_id_seq', 1, false);


--
-- Name: build_jobs_id_new_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.build_jobs_id_new_seq', 1, false);


--
-- Name: comments_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.comments_id_seq', 1, false);


--
-- Name: coupon_redemptions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.coupon_redemptions_id_seq', 1, false);


--
-- Name: coupons_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.coupons_id_seq', 1, false);


--
-- Name: currency_rates_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.currency_rates_id_seq', 1, false);


--
-- Name: inventory_change_reasons_id_new_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.inventory_change_reasons_id_new_seq', 1, false);


--
-- Name: inventory_transfer_attachments_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.inventory_transfer_attachments_id_seq', 1, false);


--
-- Name: inventory_transfer_items_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.inventory_transfer_items_id_seq', 1, false);


--
-- Name: mail_accounts_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_accounts_id_seq', 1, false);


--
-- Name: mail_automation_node_logs_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_automation_node_logs_id_seq', 1, false);


--
-- Name: mail_automation_runs_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_automation_runs_id_seq', 1, false);


--
-- Name: mail_automations_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_automations_id_seq', 1, false);


--
-- Name: mail_campaign_events_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_campaign_events_id_seq', 1, false);


--
-- Name: mail_campaigns_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_campaigns_id_seq', 1, false);


--
-- Name: mail_contacts_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_contacts_id_seq', 1, false);


--
-- Name: mail_logs_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_logs_id_seq', 1, false);


--
-- Name: mail_suppressions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_suppressions_id_seq', 1, false);


--
-- Name: mail_templates_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.mail_templates_id_seq', 1, false);


--
-- Name: membership_assignments_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.membership_assignments_id_seq', 1, false);


--
-- Name: membership_entitlements_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.membership_entitlements_id_seq', 1, false);


--
-- Name: membership_tiers_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.membership_tiers_id_seq', 1, false);


--
-- Name: order_items_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.order_items_id_seq', 1, false);


--
-- Name: order_return_items_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.order_return_items_id_seq', 1, false);


--
-- Name: order_returns_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.order_returns_id_seq', 1, false);


--
-- Name: order_shipment_items_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.order_shipment_items_id_seq', 1, false);


--
-- Name: order_status_logs_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.order_status_logs_id_seq', 1, false);


--
-- Name: orders_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.orders_id_seq', 1, false);


--
-- Name: page_views_id_seq1; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.page_views_id_seq1', 1, false);


--
-- Name: presentation_publish_attempts_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.presentation_publish_attempts_id_seq', 1, false);


--
-- Name: product_outbox_events_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.product_outbox_events_id_seq', 1, false);


--
-- Name: publication_receipts_id_new_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.publication_receipts_id_new_seq', 1, false);


--
-- Name: sys_admin_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_admin_id_seq', 1, false);


--
-- Name: sys_attachment_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_attachment_id_seq', 1, false);


--
-- Name: sys_baseline_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_baseline_id_seq', 1, false);


--
-- Name: sys_casbin_rule_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_casbin_rule_id_seq', 1, false);


--
-- Name: sys_config_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_config_id_seq', 1, false);


--
-- Name: sys_dept_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_dept_id_seq', 1, false);


--
-- Name: sys_file_category_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_file_category_id_seq', 1, false);


--
-- Name: sys_i18n_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_i18n_id_seq', 2, true);


--
-- Name: sys_media_variant_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_media_variant_id_seq', 1, false);


--
-- Name: sys_menu_permission_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_menu_permission_id_seq', 3, true);


--
-- Name: sys_menus_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_menus_id_seq', 4, true);


--
-- Name: sys_permission_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_permission_id_seq', 6, true);


--
-- Name: sys_role_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_role_id_seq', 1, false);


--
-- Name: sys_rule_assignment_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_rule_assignment_id_seq', 1, false);


--
-- Name: sys_rule_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.sys_rule_id_seq', 1, false);


--
-- Name: user_preferences_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.user_preferences_id_seq', 1, false);


--
-- Name: user_profiles_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.user_profiles_id_seq', 1, false);


--
-- Name: users_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.users_id_seq', 1, false);


--
-- Name: webhook_deliveries_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.webhook_deliveries_id_seq', 1, false);


--
-- Name: webhook_endpoints_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.webhook_endpoints_id_seq', 1, false);


--
-- PostgreSQL database dump complete
--


