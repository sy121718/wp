-- 134 · 库存与采购引用完整性。
-- 服务层之外的写入也必须无法产生悬空 product_id / variant_id。
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_stocks_product') THEN
        ALTER TABLE inventory_stocks ADD CONSTRAINT fk_inventory_stocks_product
            FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_purchase_lines_product') THEN
        ALTER TABLE inventory_purchase_order_lines ADD CONSTRAINT fk_inventory_purchase_lines_product
            FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_purchase_lines_variant') THEN
        ALTER TABLE inventory_purchase_order_lines ADD CONSTRAINT fk_inventory_purchase_lines_variant
            FOREIGN KEY (variant_id) REFERENCES product_variants(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_receipt_items_product') THEN
        ALTER TABLE inventory_purchase_receipt_items ADD CONSTRAINT fk_inventory_receipt_items_product
            FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_receipt_items_variant') THEN
        ALTER TABLE inventory_purchase_receipt_items ADD CONSTRAINT fk_inventory_receipt_items_variant
            FOREIGN KEY (variant_id) REFERENCES product_variants(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_inventory_movements_product') THEN
        ALTER TABLE inventory_stock_movements ADD CONSTRAINT fk_inventory_movements_product
            FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_product_price_adjustment_items_product') THEN
        ALTER TABLE product_price_adjustment_items ADD CONSTRAINT fk_product_price_adjustment_items_product
            FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_product_price_adjustment_items_variant') THEN
        ALTER TABLE product_price_adjustment_items ADD CONSTRAINT fk_product_price_adjustment_items_variant
            FOREIGN KEY (variant_id) REFERENCES product_variants(id) ON DELETE RESTRICT;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_inventory_stocks_variant_project
    ON inventory_stocks(project_id, variant_id);
