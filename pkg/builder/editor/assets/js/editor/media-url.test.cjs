const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(__dirname + '/media-url.js', 'utf8').replace(/export /g, '');
const context = vm.createContext({ URL }); vm.runInContext(source, context);
const normalize = context.normalizeMediaURL;
const base = 'https://media.example.test/shop/storage';

test('媒体库与历史相对路径按可信媒体基址补成完整地址', () => {
 for (const value of ['483.jpg', '/storage/483.jpg', 'storage/483.jpg']) assert.equal(normalize(value, base), base + '/483.jpg');
 assert.equal(normalize('/storage/483.jpg?v=2#photo', base), base + '/483.jpg?v=2#photo');
});
test('外链不因 storage 路径被替换域名，协议相对补完整协议', () => {
 assert.equal(normalize('https://external.test/storage/483.jpg', base), 'https://external.test/storage/483.jpg');
 assert.equal(normalize('//external.test/483.jpg', base), 'https://external.test/483.jpg');
});
test('拒绝危险协议、路径越界及缺少可信媒体基址的相对地址', () => {
 for (const value of ['javascript:alert(1)', 'data:image/png;base64,AAAA', 'blob:https://host/id', '../secret', '/uploads/a.jpg', '/storage/../secret', '/storage/%2e%2e/secret', 'https://user:pass@host/a.jpg']) assert.equal(normalize(value, base), '', value);
 assert.equal(normalize('/storage/483.jpg', ''), '');
});
test('视频、音频、字幕文件不创建图片预览', () => {
 assert.equal(context.mediaPreviewKind('https://media.test/a.mp4', 'props.url'), 'video');
 assert.equal(context.mediaPreviewKind('https://media.test/a.mp3', 'props.url'), 'audio');
 assert.equal(context.mediaPreviewKind('https://media.test/a.vtt', 'props.captionsSrc'), 'file');
 assert.equal(context.mediaPreviewKind('https://media.test/a.png', 'props.src'), 'image');
});
