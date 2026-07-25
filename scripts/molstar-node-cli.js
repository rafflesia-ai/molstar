#!/usr/bin/env node

function makeElement(tagName) {
  const element = {
    tagName: String(tagName || '').toUpperCase(),
    style: {},
    children: [],
    parentNode: null,
    classList: {
      add() {},
      remove() {},
      toggle() {},
      contains() {
        return false;
      },
    },
    appendChild(child) {
      if (child) {
        child.parentNode = element;
        element.children.push(child);
      }
      return child;
    },
    removeChild(child) {
      element.children = element.children.filter((candidate) => candidate !== child);
      if (child) child.parentNode = null;
      return child;
    },
    setAttribute(name, value) {
      element[name] = String(value);
    },
    removeAttribute(name) {
      delete element[name];
    },
    getBoundingClientRect() {
      return { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 };
    },
    requestFullscreen() {
      global.document.fullscreenElement = element;
      return Promise.resolve();
    },
    addEventListener() {},
    removeEventListener() {},
    focus() {},
    blur() {},
  };
  return element;
}

function installDomShim() {
  if (typeof global.document !== 'undefined') return;

  const body = makeElement('body');
  const head = makeElement('head');
  const documentElement = makeElement('html');

  global.document = {
    body,
    head,
    documentElement,
    scrollingElement: documentElement,
    fullscreenElement: null,
    currentScript: undefined,
    addEventListener() {},
    removeEventListener() {},
    createElement: makeElement,
    getElementsByTagName(name) {
      switch (String(name).toLowerCase()) {
        case 'body':
          return [body];
        case 'head':
          return [head];
        case 'html':
          return [documentElement];
        default:
          return [];
      }
    },
    exitFullscreen() {
      this.fullscreenElement = null;
      return Promise.resolve();
    },
  };

  global.window = {
    document: global.document,
    navigator: { userAgent: 'node' },
    location: {
      href: `file://${process.cwd().replace(/\\/g, '/')}/`,
      protocol: 'file:',
      origin: 'file://',
    },
    devicePixelRatio: 1,
    setImmediate,
    clearImmediate,
    setTimeout,
    clearTimeout,
    requestAnimationFrame(callback) {
      return setImmediate(() => callback(Date.now()));
    },
    cancelAnimationFrame(handle) {
      clearImmediate(handle);
    },
    addEventListener() {},
    removeEventListener() {},
    getComputedStyle() {
      return {
        getPropertyValue() {
          return '';
        },
      };
    },
  };
  global.navigator = global.window.navigator;
  global.location = global.window.location;
}

installDomShim();

try {
  const canvas3dModule = require('molstar/lib/commonjs/mol-canvas3d/canvas3d.js');
  const originalCreate = canvas3dModule.Canvas3D.create;
  canvas3dModule.Canvas3D.create = function createHeadlessCanvas3D(ctx, ...args) {
    if (ctx && !ctx.canvas) {
      ctx = { ...ctx, canvas: makeElement('canvas') };
    }
    return originalCreate(ctx, ...args);
  };
} catch {
  // Some Mol* CLI commands do not load Canvas3D.
}

const target = process.argv[2];
if (!target) {
  console.error('usage: molstar-node-cli.js <molstar-cli-js> [...args]');
  process.exit(2);
}

process.argv = [process.argv[0], target, ...process.argv.slice(3)];
require(require('path').resolve(target));
