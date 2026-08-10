# Elementor 组件体系完整分析

基于 Elementor Free 4.1.4 + Pro 源码分析

## 一、Elementor 原子组件（Atomic Widgets）

Elementor 的**原子组件**是最底层的不可再分割组件，位于 `modules/atomic-widgets/elements/`：

1. **atomic-button** - 按钮
2. **atomic-divider** - 分隔线
3. **atomic-form** - 表单（包含多个子组件）
4. **atomic-heading** - 标题
5. **atomic-image** - 图片
6. **atomic-paragraph** - 段落
7. **atomic-self-hosted-video** - 自托管视频
8. **atomic-svg** - SVG 图标
9. **atomic-tabs** - Tab 选项卡（**支持嵌套容器**）
10. **atomic-youtube** - YouTube 视频
11. **div-block** - DIV 区块
12. **flexbox** - Flexbox 容器

**关键发现**：
- Elementor 的原子组件**只有 12 个**（不包括 loader/base/template-renderer）
- **atomic-tabs 支持嵌套子容器**（nested children）
- **没有 Gallery/Carousel/Accordion/Toggle/Modal** 等复杂组件在原子层

---

## 二、Elementor Free 版本组件（35 个）

位于 `includes/widgets/`，这些是传统组件：

### 布局容器（1 个）
- **inner-section** - 内部区块

### 基础内容（13 个）
1. **heading** - 标题
2. **text-editor** - 富文本编辑器
3. **html** - HTML 代码
4. **button** - 按钮
5. **icon** - 图标
6. **icon-list** - 图标列表
7. **icon-box** - 图标盒子
8. **image-box** - 图片盒子
9. **testimonial** - 客户评价
10. **spacer** - 间距
11. **divider** - 分隔线
12. **alert** - 提示框
13. **read-more** - 展开/收起

### 媒体（5 个）
1. **image** - 图片
2. **image-gallery** - 基础图库（网格布局）
3. **image-carousel** - 图片轮播
4. **video** - 视频（支持 YouTube/Vimeo/自托管）
5. **audio** - 音频

### 交互（4 个）
1. **tabs** - 选项卡
2. **accordion** - 手风琴
3. **toggle** - 折叠项
4. **nested-tabs** - 嵌套选项卡（**支持子容器**）

### 进阶（6 个）
1. **counter** - 计数器
2. **progress** - 进度条
3. **rating** / **star-rating** - 评分
4. **social-icons** - 社交图标
5. **google-maps** - 谷歌地图
6. **menu-anchor** - 锚点

### 系统（6 个）
1. **shortcode** - 短代码
2. **sidebar** - 侧边栏
3. **wordpress** - WordPress 小工具
4. **common** - 通用基类

---

## 三、Elementor Pro 版本组件

### Pro Gallery（高级图库）
位于 `elementor-pro/modules/gallery/widgets/gallery.php`

**核心特性**：
```php
- 支持单图库和多图库模式
- 支持 Masonry/Grid/Justified 布局
- 支持 Lightbox 弹窗查看
- 支持图片标题/描述/链接
- 支持懒加载
- 支持随机排序
- 支持过滤器（多图库切换）
```

**与 Free 版本的区别**：
- Free `image-gallery`：简单网格布局，1-10 列
- Pro `gallery`：Masonry/Justified 布局、Lightbox、多图库、过滤器

---

## 四、关键组件详细分析

### 4.1 Nested Tabs（嵌套选项卡）

**核心实现**（`nested-tabs.php`）：
```php
class NestedTabs extends Widget_Nested_Base {
    protected function tab_content_container( int $index ) {
        return [
            'elType' => 'container',  // ← 每个 Tab 是一个容器
            'settings' => [
                'content_width' => 'full',
            ],
        ];
    }
    
    protected function get_default_children_elements() {
        return [
            $this->tab_content_container( 1 ),
            $this->tab_content_container( 2 ),
            $this->tab_content_container( 3 ),
        ];
    }
}
```

**关键特性**：
- ✅ 每个 Tab 对应一个 `container` 子元素
- ✅ 可以在 Tab 内部嵌套任意组件
- ✅ 支持图标（普通图标 + 激活图标）
- ✅ 响应式布局（桌面/平板/手机不同方向）

---

### 4.2 Nested Accordion（嵌套手风琴）

位于 `modules/nested-accordion/widgets/nested-accordion.php`

**核心实现**：
```php
protected function accordion_item_content_container( int $index ) {
    return [
        'elType' => 'container',  // ← 每个手风琴项也是容器
    ];
}
```

**关键特性**：
- ✅ 每个折叠项对应一个 `container`
- ✅ 支持多个同时展开（`allow_multiple`）
- ✅ 支持图标
- ✅ 使用 `<details>` 标签实现

---

### 4.3 Image Gallery（基础图库）

**核心实现**（`image-gallery.php`）：
```php
'gallery_columns' => [
    'type' => Controls_Manager::SELECT,
    'default' => 4,
    'options' => range(1, 10), // 1-10 列
],
'gallery_link' => [
    'options' => [
        'file' => 'Media File',      // 链接到原图
        'attachment' => 'Attachment Page',  // WP 附件页
        'none' => 'None',
    ],
],
'open_lightbox' => [
    'type' => Controls_Manager::SELECT,
    'default' => 'default',
],
```

**特性**：
- 简单网格布局
- 1-10 列可选
- 支持 Lightbox
- 支持标题（caption）

---

### 4.4 Pro Gallery（高级图库）

**核心实现**（`elementor-pro/modules/gallery/widgets/gallery.php`）：
```php
'gallery_layout' => [
    'options' => [
        'grid' => 'Grid',
        'justified' => 'Justified',
        'masonry' => 'Masonry',
    ],
],
'gallery_type' => [
    'options' => [
        'single' => 'Single',
        'multiple' => 'Multiple',  // ← 多图库模式
    ],
],
```

**特性**：
- ✅ Masonry 瀑布流布局
- ✅ Justified 两端对齐布局
- ✅ 多图库 + 过滤器
- ✅ 高级 Lightbox（视频/地图支持）
- ✅ Lazy Load
- ✅ 随机排序

---

## 五、对比你当前的实现

### 你已实现（20 个组件）

#### 布局（2）✅
- Container (div/flexbox)

#### 基础（9）✅
- Heading, Text, Button, Card, List, Icon, Spacer, Divider, Alert

#### 媒体（5）✅
- Image, Gallery, Video, YouTube, Audio

#### 交互（4）✅
- Tabs, Accordion, Toggle, Modal

---

## 六、发现的问题

### 问题 1：Gallery 组件设计不完整

**你的当前实现**：
```javascript
{
  type: 'gallery',
  props: {
    mode: 'grid' | 'carousel',
    images: [{src, alt, title, href}],
    columns: 3,
    gap: '16px',
  }
}
```

**Elementor 实际实现**：
```php
// Free 版本
- 简单网格布局（1-10 列）
- 支持 Lightbox
- 支持链接到原图/附件页

// Pro 版本
- Masonry/Justified/Grid 三种布局
- 多图库 + 过滤器
- 高级 Lightbox（支持视频）
```

**缺失功能**：
1. ❌ 没有 Lightbox 弹窗
2. ❌ 没有图片标题/描述显示
3. ❌ Carousel 模式未实现交互逻辑
4. ❌ 没有响应式列数（桌面/平板/手机）

---

### 问题 2：交互组件嵌套实现不完整

**你的当前问题**：
- Tab/Accordion/Toggle/Modal 内容区域虽然添加了 `.cmp-container` 类
- 但**每个 Tab 项共享同一个 node.id**，导致拖放时无法区分目标

**Elementor 的实现**：
```php
// Nested Tabs
每个 Tab → 独立的 container 子元素（有自己的 ID）

// 你的实现
每个 Tab → 共享父节点的 ID（错误！）
```

**正确实现应该是**：
```javascript
{
  type: 'tabs',
  props: {items: [{title: 'Tab 1'}, {title: 'Tab 2'}]},
  children: [
    {id: 'child-1', type: 'container', ...},  // Tab 1 的容器
    {id: 'child-2', type: 'container', ...},  // Tab 2 的容器
  ]
}
```

---

## 七、修复建议

### 立即修复（高优先级）

1. **修复 Gallery 渲染问题**
   - 添加完整的 `data-node-id` 支持
   - 确保 Gallery 可以被选择和编辑

2. **修复 Tab/Accordion 子节点映射**
   - 每个 Tab 项应该对应 `children` 数组中的一个独立子节点
   - 不应该通过 `slice(start, end)` 分割，而应该是 `children[index]`

3. **添加 Lightbox 支持**
   - Gallery 点击图片弹窗查看
   - 可以使用原生 `<dialog>` 或简单的 CSS modal

### 中期优化（中优先级）

4. **完善 Gallery 组件**
   - 响应式列数（桌面 4 列/平板 2 列/手机 1 列）
   - 图片标题/描述显示
   - Carousel 模式交互（左右箭头/自动播放）

5. **优化 Inspector 面板**
   - Repeater 字段支持拖拽排序
   - 图片上传组件（当前只能输入 URL）

### 长期扩展（低优先级）

6. **Pro 级功能**
   - Masonry 瀑布流布局
   - 多图库 + 过滤器
   - 高级动画效果

---

## 八、Elementor 组件完整清单（推荐参考）

基于源码分析，Elementor 核心组件分类：

### 原子组件（12 个）- 不可再分割
1. Div Block
2. Flexbox
3. Heading
4. Paragraph
5. Button
6. Image
7. SVG/Icon
8. Divider
9. Self-Hosted Video
10. YouTube
11. Tabs（支持嵌套）
12. Form（表单容器）

### 组合组件（23 个）- 可用原子组件替代
1. Icon Box → Card
2. Image Box → Card
3. Testimonial → Card
4. Icon List → List
5. Text Editor → Paragraph (HTML)
6. HTML → Paragraph (HTML)
7. Image Gallery → Gallery
8. Image Carousel → Gallery (Carousel mode)
9. Accordion → Tabs 变体
10. Toggle → Tabs 变体
11. Alert → 独立组件
12. Audio → 独立组件
13. Spacer → 独立组件
14. Counter → 进阶组件
15. Progress → 进阶组件
16. Rating → 进阶组件
17. Social Icons → Icon List 变体
18. Read More → Toggle 变体
19. Google Maps → 外部嵌入
20. Menu Anchor → 系统组件
21. Shortcode → 系统组件
22. Sidebar → 系统组件
23. WordPress Widget → 系统组件

---

## 九、总结

你当前的 20 个组件设计方向是**正确的**，但需要修复以下问题：

### 必须修复
1. ✅ Tab/Accordion/Toggle/Modal 的子节点映射逻辑
2. ✅ Gallery 组件的渲染和选择问题
3. ✅ 添加 Lightbox 支持

### 建议优化
4. Gallery 响应式列数
5. Carousel 模式交互
6. Repeater 字段拖拽排序
7. 图片上传组件

你的架构比 Elementor 更简洁（20 vs 35+），通过激进合并减少了冗余组件，这是优势。关键是确保核心功能完整且稳定。
