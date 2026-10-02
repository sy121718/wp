// 由 go run ./cmd/workbench-contracts 生成，请修改组件 Go 声明。
// 此文件随源码提交；检查：go run ./cmd/workbench-contracts -check
export const alignedRepeaters = {
    "core.accordion": {
        "type": "core.accordion",
        "alignKey": "items",
        "field": "title",
        "noun": "折叠项",
        "label": "标题",
        "addText": "+ 添加折叠项（自动创建内容）",
        "extra": [
            {
                "key": "open",
                "label": "默认展开"
            }
        ]
    },
    "core.tabs": {
        "type": "core.tabs",
        "alignKey": "tabs",
        "field": "label",
        "noun": "页签",
        "label": "标签",
        "addText": "+ 添加页签（自动创建面板）"
    }
};

export const paletteSpec = {
    "items": {
        "core.accordion": {
            "type": "core.accordion",
            "displayName": "手风琴",
            "hint": "折叠展开",
            "category": "basic",
            "defaultProps": {
                "items": [
                    {
                        "open": true,
                        "title": "折叠项一"
                    }
                ]
            }
        },
        "core.addToCart": {
            "type": "core.addToCart",
            "displayName": "加购按钮",
            "hint": "加入购物车（提交到购物车片段）",
            "category": "basic",
            "defaultProps": {
                "buttonText": "加入购物车",
                "cartTarget": "#cart",
                "currency": "¥",
                "optionsField": "product.options",
                "showQuantity": false,
                "source": "product",
                "variantMode": "single",
                "variantsField": "product.variants"
            }
        },
        "core.articleList": {
            "type": "core.articleList",
            "displayName": "文章列表",
            "hint": "按集合源自动出文章卡片",
            "category": "basic",
            "defaultProps": {
                "collectionLimit": 6,
                "columns": "3",
                "layout": "grid",
                "showExcerpt": "on",
                "showImage": "on",
                "titleTag": "h3"
            }
        },
        "core.badge": {
            "type": "core.badge",
            "displayName": "徽章",
            "hint": "文本徽章",
            "category": "basic",
            "defaultProps": {
                "text": "新品",
                "variant": "solid"
            }
        },
        "core.breadcrumb": {
            "type": "core.breadcrumb",
            "displayName": "面包屑",
            "hint": "当前页层级（自动按路径派生）",
            "category": "basic"
        },
        "core.button": {
            "type": "core.button",
            "displayName": "按钮",
            "hint": "行动按钮",
            "category": "basic",
            "defaultProps": {
                "action": "internal",
                "text": "了解更多",
                "value": "/"
            }
        },
        "core.card": {
            "type": "core.card",
            "displayName": "卡片",
            "hint": "标题+正文+按钮",
            "category": "basic",
            "defaultProps": {
                "buttonLink": "/",
                "buttonText": "了解更多",
                "text": "卡片正文内容。",
                "title": "卡片标题"
            }
        },
        "core.cardstack": {
            "type": "core.cardstack",
            "displayName": "卡片堆叠",
            "hint": "悬停扇形/直排 · 滚动堆叠",
            "category": "basic",
            "defaultProps": {
                "count": 9,
                "hueStep": 50,
                "shape": "fan",
                "spreadAngle": 5,
                "spreadDistance": 120,
                "trigger": "hover"
            }
        },
        "core.cartIcon": {
            "type": "core.cartIcon",
            "displayName": "购物车图标",
            "hint": "页头购物车入口（下拉 / 抽屉 / 弹窗 / 悬停）",
            "category": "basic",
            "defaultProps": {
                "align": "right",
                "drawerSide": "right",
                "icon": "shopping-cart",
                "label": "购物车",
                "mode": "dropdown",
                "showCount": true,
                "showLabel": false
            }
        },
        "core.checkoutForm": {
            "type": "core.checkoutForm",
            "displayName": "结算表单",
            "hint": "购物车结算：字段来自订单契约，标签与顺序可配",
            "category": "basic",
            "defaultProps": {
                "collectBilling": false,
                "fields": [
                    {
                        "key": "email"
                    },
                    {
                        "key": "name"
                    },
                    {
                        "key": "phone"
                    },
                    {
                        "key": "country"
                    },
                    {
                        "key": "province"
                    },
                    {
                        "key": "city"
                    },
                    {
                        "key": "district"
                    },
                    {
                        "key": "address"
                    },
                    {
                        "key": "zip"
                    },
                    {
                        "key": "remark"
                    },
                    {
                        "key": "billName"
                    },
                    {
                        "key": "billPhone"
                    },
                    {
                        "key": "billCountry"
                    },
                    {
                        "key": "billProvince"
                    },
                    {
                        "key": "billCity"
                    },
                    {
                        "key": "billDistrict"
                    },
                    {
                        "key": "billAddress"
                    },
                    {
                        "key": "billZip"
                    }
                ],
                "submitLabel": "提交订单"
            }
        },
        "core.container": {
            "type": "core.container",
            "displayName": "容器",
            "hint": "布局容器",
            "category": "basic",
            "defaultProps": {
                "box": {
                    "padding": {
                        "desktop": "32px"
                    }
                },
                "layout": {
                    "engine": "flex",
                    "flex": {
                        "direction": "column",
                        "gap": "16px"
                    }
                },
                "tag": "section"
            }
        },
        "core.countdown": {
            "type": "core.countdown",
            "displayName": "倒计时",
            "hint": "营销倒计时",
            "category": "basic",
            "defaultProps": {
                "showDays": true,
                "targetDate": "2030-01-01 00:00:00"
            }
        },
        "core.counter": {
            "type": "core.counter",
            "displayName": "计数器",
            "hint": "数字统计",
            "category": "basic",
            "defaultProps": {
                "end": 100,
                "start": 0,
                "suffix": "+"
            }
        },
        "core.divider": {
            "type": "core.divider",
            "displayName": "分隔线",
            "hint": "内容分隔",
            "category": "basic",
            "defaultProps": {
                "style": "solid",
                "weight": "1px"
            }
        },
        "core.faq": {
            "type": "core.faq",
            "displayName": "常见问题",
            "hint": "问答折叠",
            "category": "basic",
            "defaultProps": {
                "items": [
                    {
                        "answer": "这里是回答内容。",
                        "open": true,
                        "question": "常见问题一？"
                    },
                    {
                        "answer": "这里是回答内容。",
                        "question": "常见问题二？"
                    }
                ]
            }
        },
        "core.form": {
            "type": "core.form",
            "displayName": "表单",
            "hint": "联系/订阅表单",
            "category": "basic",
            "defaultProps": {
                "fields": [
                    {
                        "label": "姓名",
                        "name": "name",
                        "required": true,
                        "type": "text"
                    },
                    {
                        "label": "邮箱",
                        "name": "email",
                        "required": true,
                        "type": "email"
                    }
                ],
                "submitLabel": "提交"
            }
        },
        "core.gallery": {
            "type": "core.gallery",
            "displayName": "图集",
            "hint": "图片网格 / 轮播",
            "category": "basic",
            "defaultProps": {
                "aspectRatio": "16:9",
                "grid": {
                    "columns": {
                        "desktop": 3
                    }
                },
                "items": [
                    {
                        "alt": "图集占位图",
                        "url": "https://placehold.co/1200x800/png"
                    }
                ],
                "mode": "grid",
                "objectFit": "cover",
                "radius": "8px"
            }
        },
        "core.heading": {
            "type": "core.heading",
            "displayName": "标题",
            "hint": "文字标题",
            "category": "basic",
            "defaultProps": {
                "tag": "h2",
                "text": "新标题"
            }
        },
        "core.icon": {
            "type": "core.icon",
            "displayName": "图标",
            "hint": "通用 SVG 图标",
            "category": "basic",
            "defaultProps": {
                "iconName": "star",
                "size": "24px"
            }
        },
        "core.image": {
            "type": "core.image",
            "displayName": "图片",
            "hint": "外部图片",
            "category": "basic",
            "defaultProps": {
                "alt": "图片占位符",
                "objectFit": "cover",
                "src": "https://placehold.co/1200x800/png",
                "width": "100%"
            }
        },
        "core.infobox": {
            "type": "core.infobox",
            "displayName": "信息框",
            "hint": "图标+标题+文本",
            "category": "basic",
            "defaultProps": {
                "align": "center",
                "icon": "shield",
                "text": "一句话描述你的服务或卖点。",
                "title": "信息框标题"
            }
        },
        "core.languages": {
            "type": "core.languages",
            "displayName": "语言切换",
            "hint": "多语言站点切换链接",
            "category": "basic",
            "defaultProps": {
                "gap": "16px",
                "orientation": "horizontal"
            }
        },
        "core.list": {
            "type": "core.list",
            "displayName": "列表",
            "hint": "图标/序号/圆点列表",
            "category": "basic",
            "defaultProps": {
                "items": [
                    {
                        "icon": "check",
                        "text": "列表项内容"
                    }
                ],
                "style": "icon"
            }
        },
        "core.loader": {
            "type": "core.loader",
            "displayName": "加载指示",
            "hint": "纯 CSS 加载动画（九种形态）",
            "category": "basic",
            "defaultProps": {
                "size": "32px",
                "variant": "spinner"
            }
        },
        "core.marquee": {
            "type": "core.marquee",
            "displayName": "跑马灯",
            "hint": "无缝滚动内容",
            "category": "basic",
            "defaultProps": {
                "direction": "left",
                "gap": "24px",
                "speed": 12
            }
        },
        "core.nav": {
            "type": "core.nav",
            "displayName": "导航菜单",
            "hint": "站点菜单（支持二级）",
            "category": "basic",
            "defaultProps": {
                "color": "#3B3C40",
                "gap": "24px",
                "hoverColor": "#D93425",
                "itemPadding": "8px 0",
                "items": [
                    {
                        "label": "首页",
                        "url": "/"
                    },
                    {
                        "children": [
                            {
                                "label": "一次性",
                                "url": "/shop/disposable"
                            },
                            {
                                "label": "换弹",
                                "url": "/shop/pods"
                            }
                        ],
                        "label": "产品",
                        "url": "/shop"
                    },
                    {
                        "label": "关于我们",
                        "url": "/about"
                    }
                ],
                "mobileCollapse": true,
                "orientation": "horizontal"
            }
        },
        "core.orderList": {
            "type": "core.orderList",
            "displayName": "我的订单",
            "hint": "访客订单列表（登录后可见，片段现拉）",
            "category": "basic",
            "defaultProps": {
                "pageSize": 10,
                "showTitle": true,
                "title": "我的订单"
            }
        },
        "core.product": {
            "type": "core.product",
            "displayName": "商品详情",
            "hint": "吃商品数据的详情组件",
            "category": "basic",
            "defaultProps": {
                "currency": "¥",
                "descriptionField": "product.description",
                "galleryField": "product.images",
                "mediaField": "product.defaultImage",
                "priceField": "product.priceRange",
                "source": "product",
                "subtitleField": "product.subtitle",
                "titleField": "product.name",
                "titleTag": "h2"
            }
        },
        "core.productCard": {
            "type": "core.productCard",
            "displayName": "商品卡",
            "hint": "吃商品数据的最小展示单元（可作集合卡模板）",
            "category": "basic",
            "defaultProps": {
                "comparePriceField": "item.comparePrice",
                "currency": "¥",
                "imageField": "item.images",
                "linkField": "item.url",
                "linkPrefix": "",
                "priceField": "item.priceRange",
                "tagsField": "item.tags",
                "titleField": "item.name",
                "titleTag": "h3"
            }
        },
        "core.productList": {
            "type": "core.productList",
            "displayName": "商品列表",
            "hint": "网格 / 列表铺开一批商品",
            "category": "basic",
            "defaultProps": {
                "collectionLimit": 8,
                "columns": "auto",
                "comparePriceField": "item.comparePrice",
                "currency": "¥",
                "emptyText": "暂无商品",
                "filterStatus": "published",
                "imageField": "item.images",
                "layout": "grid",
                "linkField": "item.url",
                "linkPrefix": "",
                "priceField": "item.priceRange",
                "tagsField": "item.tags",
                "titleField": "item.name",
                "titleTag": "h3"
            }
        },
        "core.productSelector": {
            "type": "core.productSelector",
            "displayName": "规格选择器",
            "hint": "选规格切组合（可放详情页任意位置）",
            "category": "basic",
            "defaultProps": {
                "currency": "¥",
                "emptyText": "该商品暂无可选规格",
                "optionsField": "product.options",
                "variantsField": "product.variants"
            }
        },
        "core.progress": {
            "type": "core.progress",
            "displayName": "进度条",
            "hint": "数据进度",
            "category": "basic",
            "defaultProps": {
                "label": "完成度",
                "max": 100,
                "value": 60
            }
        },
        "core.quote": {
            "type": "core.quote",
            "displayName": "引用",
            "hint": "引用块",
            "category": "basic",
            "defaultProps": {
                "align": "left",
                "author": "作者名",
                "text": "引用一段有力量的话。"
            }
        },
        "core.rating": {
            "type": "core.rating",
            "displayName": "评分",
            "hint": "星形评分",
            "category": "basic",
            "defaultProps": {
                "max": 5,
                "value": 4.5
            }
        },
        "core.searchResults": {
            "type": "core.searchResults",
            "displayName": "站内搜索",
            "hint": "搜索框 + 结果列表（片段现拉）",
            "category": "basic",
            "defaultProps": {
                "limit": 8,
                "placeholder": "搜索站内内容"
            }
        },
        "core.shapedivider": {
            "type": "core.shapedivider",
            "displayName": "形状分隔线",
            "hint": "区块过渡装饰（波浪/弧线/斜坡）",
            "category": "basic",
            "defaultProps": {
                "shape": "wave"
            }
        },
        "core.slider": {
            "type": "core.slider",
            "displayName": "轮播",
            "hint": "多屏滑动（可嵌套）",
            "category": "basic",
            "defaultProps": {
                "autoplay": 0,
                "gap": "16px",
                "perView": {
                    "desktop": 1
                },
                "showArrows": true,
                "showDots": true
            }
        },
        "core.social_buttons": {
            "type": "core.social_buttons",
            "displayName": "社交图标",
            "hint": "社交平台图标组",
            "category": "basic",
            "defaultProps": {
                "color": "brand",
                "items": [
                    {
                        "platform": "facebook",
                        "url": "https://facebook.com"
                    },
                    {
                        "platform": "x",
                        "url": "https://x.com"
                    },
                    {
                        "platform": "instagram",
                        "url": "https://instagram.com"
                    }
                ],
                "shape": "circle",
                "size": "40px"
            }
        },
        "core.spacer": {
            "type": "core.spacer",
            "displayName": "间隔",
            "hint": "留白空间",
            "category": "basic",
            "defaultProps": {
                "height": {
                    "desktop": "32px"
                }
            }
        },
        "core.table": {
            "type": "core.table",
            "displayName": "表格",
            "hint": "数据表格",
            "category": "basic",
            "defaultProps": {
                "bordered": true,
                "caption": "数据表格",
                "headers": [
                    "列一",
                    "列二"
                ],
                "rows": [
                    [
                        "A",
                        "B"
                    ],
                    [
                        "C",
                        "D"
                    ]
                ],
                "striped": true
            }
        },
        "core.tabs": {
            "type": "core.tabs",
            "displayName": "页签",
            "hint": "多面板切换",
            "category": "basic",
            "defaultProps": {
                "tabs": [
                    {
                        "label": "页签一"
                    }
                ]
            }
        },
        "core.text": {
            "type": "core.text",
            "displayName": "文本",
            "hint": "正文段落",
            "category": "basic",
            "defaultProps": {
                "mode": "plaintext",
                "plainTag": "p",
                "text": "在这里输入正文内容。"
            }
        },
        "core.userForms": {
            "type": "core.userForms",
            "displayName": "账号表单",
            "hint": "登录 / 注册 / 找回密码 / 账号面板（片段现拉）",
            "category": "basic",
            "defaultProps": {
                "mode": "login",
                "next": "",
                "showTitle": true,
                "title": "登录"
            }
        },
        "core.video": {
            "type": "core.video",
            "displayName": "视频",
            "hint": "外链嵌入/本地 MP4",
            "category": "basic",
            "defaultProps": {
                "controls": true,
                "ratio": "16:9",
                "url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
            }
        }
    }
};
