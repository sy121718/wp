/* 商品显示转换：评分、描述回填与规格文案都只消费现成数据。 */
(function (global) {
  "use strict";
  function formatSpec(values, attributes) {
    values = values || {};
    var parts = [], used = new Set();
    (attributes || []).forEach(function (attribute) {
      if (!Object.prototype.hasOwnProperty.call(values, attribute.key)) return;
      used.add(attribute.key);
      var value = (attribute.values || []).find(function (item) { return item.key === values[attribute.key]; });
      parts.push(attribute.name + " " + (value ? value.label : values[attribute.key]));
    });
    Object.keys(values).filter(function (key) { return !used.has(key); }).sort().forEach(function (key) {
      parts.push(key + " " + values[key]);
    });
    return parts.join(" · ") || "—";
  }
  var api = global.WBProductDisplay = { formatSpec: formatSpec, attributes: [] };
  function init() {
    global.document.querySelectorAll("[data-product-rating]").forEach(function (node) {
      var score = Number(node.dataset.productRating);
      if (Number.isFinite(score)) node.textContent = score.toFixed(2);
    });
    var source = global.document.getElementById("product-display-data");
    if (!source || !global.WBUI.markOnce(source, "ProductDisplay")) return;
    var product = JSON.parse(source.textContent);
    api.attributes = product.attributes || [];
    var description = product.description;
    var input = global.document.getElementById("rich-descriptionHtml-product");
    if (input) {
      var html = typeof description === "string" ? description : (description && description.html || "");
      input.value = html;
      var editor = global.document.querySelector('trix-editor[input="' + input.id + '"]');
      if (editor && editor.editor) editor.editor.loadHTML(html);
    }
    var variants = new Map((product.variants || []).map(function (variant) { return [variant.id, variant]; }));
    global.document.querySelectorAll("[data-product-spec]").forEach(function (node) {
      var variant = variants.get(node.dataset.productSpec);
      if (variant) node.textContent = formatSpec(variant.optionValues, api.attributes);
    });
  }
  global.WBUI.register(init);
})(window);
