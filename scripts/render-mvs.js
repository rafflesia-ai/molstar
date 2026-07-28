#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const readline = require('readline');

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

const canvas3dModule = require('molstar/lib/commonjs/mol-canvas3d/canvas3d.js');
const originalCreate = canvas3dModule.Canvas3D.create;
canvas3dModule.Canvas3D.create = function createHeadlessCanvas3D(ctx, ...args) {
  if (ctx && !ctx.canvas) {
    ctx = { ...ctx, canvas: makeElement('canvas') };
  }
  return originalCreate(ctx, ...args);
};

const gl = require('gl');
const jpegjs = require('jpeg-js');
const pngjs = require('pngjs');
const canvas = require('canvas');
const { Canvas3DParams } = require('molstar/lib/commonjs/mol-canvas3d/canvas3d.js');
const { setCanvasModule } = require('molstar/lib/commonjs/mol-geo/geometry/text/font-atlas.js');
const { HeadlessPluginContext } = require('molstar/lib/commonjs/mol-plugin/headless-plugin-context.js');
const { DefaultPluginSpec, PluginSpec } = require('molstar/lib/commonjs/mol-plugin/spec.js');
const { PluginStateObject } = require('molstar/lib/commonjs/mol-plugin-state/objects.js');
const { StructureElement, StructureProperties } = require('molstar/lib/commonjs/mol-model/structure.js');
const { defaultCanvas3DParams } = require('molstar/lib/commonjs/mol-plugin/util/headless-screenshot.js');
const { Task } = require('molstar/lib/commonjs/mol-task/index.js');
const { setFSModule } = require('molstar/lib/commonjs/mol-util/data-source.js');
const { onelinerJsonString } = require('molstar/lib/commonjs/mol-util/json.js');
const { ParamDefinition } = require('molstar/lib/commonjs/mol-util/param-definition.js');
const { Mp4Export } = require('molstar/lib/commonjs/extensions/mp4-export/index.js');
// Provides the `plddt-confidence` color theme and the pLDDT/QMEAN model
// properties that AlphaFold and ModelArchive structures carry.
const { MAQualityAssessment } = require('molstar/lib/commonjs/extensions/model-archive/quality-assessment/behavior.js');
const { MolViewSpec } = require('molstar/lib/commonjs/extensions/mvs/behavior.js');
const { loadMVSX } = require('molstar/lib/commonjs/extensions/mvs/components/formats.js');
const { loadMVS } = require('molstar/lib/commonjs/extensions/mvs/load.js');
const { MVSData } = require('molstar/lib/commonjs/extensions/mvs/mvs-data.js');
const { createMVSRefMap } = require('molstar/lib/commonjs/extensions/mvs/util.js');

// Mol*'s Node file:// reader turns a URL into a filename with a bare
// `url.substring('file://'.length)`, so it never percent-decodes. A local input
// under a path containing a space, a non-ASCII character, or a parenthesis is
// written as a correctly-escaped RFC 8089 URL and then fails to open with
// ENOENT. Decode on the fallback so those paths load, while a path that really
// does contain a "%" keeps working.
const fsWithPercentDecodedPaths = {
  ...fs,
  readFile(filePath, ...rest) {
    const callback = rest[rest.length - 1];
    if (typeof filePath !== 'string' || typeof callback !== 'function' || !filePath.includes('%')) {
      return fs.readFile(filePath, ...rest);
    }
    let decoded = filePath;
    try {
      decoded = decodeURIComponent(filePath);
    } catch {
      return fs.readFile(filePath, ...rest);
    }
    if (decoded === filePath) return fs.readFile(filePath, ...rest);
    return fs.readFile(filePath, ...rest.slice(0, -1), (error, data) => {
      if (!error) return callback(error, data);
      fs.readFile(decoded, ...rest.slice(0, -1), (fallbackError, fallbackData) => {
        // Report the original error when the decoded path is no better, so the
        // message still names the path Mol* was actually given.
        if (fallbackError) return callback(error, undefined);
        callback(null, fallbackData);
      });
    });
  },
};

setFSModule(fsWithPercentDecodedPaths);
setCanvasModule(canvas);

function parseArgs(argv) {
  const args = {
    input: '',
    output: '',
    size: { width: 800, height: 800 },
    molj: false,
    noExtensions: false,
    format: undefined,
    jpegQuality: 90,
    quiet: false,
    json: false,
  };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    switch (arg) {
      case '-i':
      case '--input':
        args.input = requireValue(argv, ++i, arg);
        break;
      case '-o':
      case '--output':
        args.output = requireValue(argv, ++i, arg);
        break;
      case '-s':
      case '--size':
        args.size = parseSize(requireValue(argv, ++i, arg));
        break;
      case '-m':
      case '--molj':
        args.molj = true;
        break;
      case '-n':
      case '--no-extensions':
        args.noExtensions = true;
        break;
      case '--format':
        args.format = normalizeFormat(requireValue(argv, ++i, arg));
        break;
      case '--jpeg-quality':
        args.jpegQuality = parseInteger(requireValue(argv, ++i, arg), arg);
        break;
      case '--quiet':
        args.quiet = true;
        break;
      case '--json':
        args.json = true;
        break;
      case '-h':
      case '--help':
        printHelp();
        process.exit(0);
      default:
        throw new Error(`unknown argument: ${arg}`);
    }
  }
  if (!args.input) throw new Error('missing required --input');
  if (!args.output) throw new Error('missing required --output');
  return args;
}

function requireValue(argv, index, flag) {
  const value = argv[index];
  if (!value || value.startsWith('-')) throw new Error(`${flag} requires a value`);
  return value;
}

function parseSize(value) {
  const parts = String(value).toLowerCase().split('x');
  if (parts.length !== 2) throw new Error(`invalid --size ${value}, expected WIDTHxHEIGHT`);
  const width = parseInteger(parts[0], '--size width');
  const height = parseInteger(parts[1], '--size height');
  return { width, height };
}

function parseInteger(value, label) {
  if (!/^\d+$/.test(String(value))) throw new Error(`${label} must be a positive integer`);
  const n = Number(value);
  if (!Number.isSafeInteger(n) || n <= 0) throw new Error(`${label} must be a positive integer`);
  return n;
}

function normalizeFormat(value) {
  const format = String(value).toLowerCase();
  if (format === 'jpg') return 'jpeg';
  if (format !== 'png' && format !== 'jpeg') throw new Error(`invalid --format ${value}`);
  return format;
}

function printHelp() {
  process.stdout.write(`usage: render-mvs --input scene.mvsj --output out.png [options]

Options:
  -i, --input PATH          Input .mvsj or .mvsx file
  -o, --output PATH         Output .png, .jpg, .jpeg, or .mp4 path
  -s, --size WIDTHxHEIGHT   Output size, default 800x800
  -m, --molj                Save Mol* state next to the output as .molj
  -n, --no-extensions       Disable builtin MVS loading extensions
      --format png|jpeg     Override image format
      --jpeg-quality N      JPEG quality, default 90
      --quiet               Suppress progress logs
      --json                Emit a JSON success or error report
      --inspect             Inspect the loaded Mol* scene instead of rendering
`);
}

function parseInspectArgs(argv) {
  const args = {
    input: '',
    noExtensions: false,
    quiet: false,
    json: false,
  };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    switch (arg) {
      case '--inspect':
        break;
      case '-i':
      case '--input':
        args.input = requireValue(argv, ++i, arg);
        break;
      case '-n':
      case '--no-extensions':
        args.noExtensions = true;
        break;
      case '--quiet':
        args.quiet = true;
        break;
      case '--json':
        args.json = true;
        break;
      case '-h':
      case '--help':
        printHelp();
        process.exit(0);
      default:
        throw new Error(`unknown argument: ${arg}`);
    }
  }
  if (!args.input) throw new Error('missing required --input');
  return args;
}

async function createPlugin(size) {
  probeWebGLContext();
  const externalModules = { gl, pngjs, 'jpeg-js': jpegjs };
  const spec = DefaultPluginSpec();
  spec.behaviors.push(PluginSpec.Behavior(MolViewSpec));
  spec.behaviors.push(PluginSpec.Behavior(Mp4Export));
  spec.behaviors.push(PluginSpec.Behavior(MAQualityAssessment));
  const headlessCanvasOptions = defaultCanvas3DParams();
  const canvasOptions = {
    ...ParamDefinition.getDefaultValues(Canvas3DParams),
    cameraResetDurationMs: headlessCanvasOptions.cameraResetDurationMs,
    postprocessing: headlessCanvasOptions.postprocessing,
  };
  const plugin = new HeadlessPluginContext(externalModules, spec, size, { canvas: canvasOptions });
  try {
    await plugin.init();
    return plugin;
  } catch (error) {
    plugin.dispose();
    throw error;
  }
}

function webglUnavailableError(detail) {
  const suffix = detail ? `: ${detail}` : '';
  const error = new Error(
    `headless WebGL context is unavailable${suffix}. ` +
      'Use xvfb with software GL in Docker, set LIBGL_ALWAYS_SOFTWARE=1 when using Mesa, or run non-render checks with --dry-run / inspect --semantic=false.',
  );
  error.code = 'HEADLESS_WEBGL_UNAVAILABLE';
  return error;
}

function probeWebGLContext() {
  let context = null;
  try {
    context = gl(8, 8, { preserveDrawingBuffer: true });
  } catch (error) {
    throw webglUnavailableError(error && error.message ? error.message : String(error));
  }
  if (!context) {
    throw webglUnavailableError('gl returned null');
  }
  if (typeof context.destroy === 'function') context.destroy();
}

async function loadScene(plugin, input, noExtensions) {
  if (input.toLowerCase().endsWith('.mvsj')) {
    const data = fs.readFileSync(input, { encoding: 'utf8' });
    await loadMVS(plugin, MVSData.fromMVSJ(data), {
      sanityChecks: true,
      sourceUrl: `file://${path.resolve(input)}`,
      extensions: noExtensions ? [] : undefined,
    });
    return;
  }
  if (input.toLowerCase().endsWith('.mvsx')) {
    const data = fs.readFileSync(input);
    const mvsx = await plugin.runTask(Task.create('Load MVSX', async (ctx) => loadMVSX(plugin, ctx, data)));
    await loadMVS(plugin, mvsx.mvsData, {
      sanityChecks: true,
      sourceUrl: mvsx.sourceUrl,
      extensions: noExtensions ? [] : undefined,
    });
    return;
  }
  throw new Error(`input must end with .mvsj or .mvsx: ${input}`);
}

function withExtension(filename, extension) {
  const oldExtension = path.extname(filename);
  return filename.slice(0, filename.length - oldExtension.length) + extension;
}

function collectBadCells(plugin) {
  const cells = Array.from(plugin.state.data.cells.values());
  return cells
    .filter((cell) => cell.status !== 'ok')
    .map((cell) => ({
      transformer: cell.transform.transformer.id,
      params: cell.transform.params,
      error: cell.errorText,
      status: cell.status,
    }));
}

function checkState(plugin) {
  const badCells = collectBadCells(plugin);
  if (badCells.length > 0) {
    throw new Error(`building Mol* state failed: ${onelinerJsonString(badCells[0])}`);
  }
  return badCells;
}

function rendererCapabilities() {
  let molstarVersion = 'unknown';
  try {
    molstarVersion = require('molstar/package.json').version;
  } catch {
    // keep unknown
  }
  let glContext = false;
  let glError = '';
  try {
    probeWebGLContext();
    glContext = true;
  } catch (error) {
    glError = error && error.message ? error.message : String(error);
  }
  let canvas2d = false;
  let canvasError = '';
  try {
    const probe = canvas.createCanvas(8, 8);
    canvas2d = Boolean(probe && probe.getContext('2d'));
  } catch (error) {
    canvasError = error && error.message ? error.message : String(error);
  }
  return {
    ok: true,
    protocol: 'headlessmolstar-worker-v1',
    node: process.version,
    platform: process.platform,
    arch: process.arch,
    pid: process.pid,
    molstar: molstarVersion,
    modules: {
      gl: Boolean(gl),
      pngjs: Boolean(pngjs),
      jpegjs: Boolean(jpegjs),
      canvas: Boolean(canvas),
      mp4: Boolean(Mp4Export),
      mvs: Boolean(MolViewSpec),
    },
    gl: {
      available: glContext,
      error: glError || undefined,
    },
    canvas: {
      available: canvas2d,
      error: canvasError || undefined,
    },
  };
}

async function renderWithPlugin(plugin, args) {
  try {
    await loadScene(plugin, args.input, args.noExtensions);
    fs.mkdirSync(path.dirname(args.output), { recursive: true });
    if (args.molj) await plugin.saveStateSnapshot(withExtension(args.output, '.molj'));
    if (args.output.toLowerCase().endsWith('.mp4')) {
      await plugin.saveAnimation(args.output, { size: args.size });
    } else {
      await plugin.saveImage(args.output, args.size, undefined, args.format, args.jpegQuality);
    }
    const badCells = checkState(plugin);
    return { ok: true, input: args.input, output: args.output, size: args.size, badCells };
  } finally {
    await plugin.clear();
  }
}

async function renderOnce(args) {
  const plugin = await createPlugin(args.size);
  try {
    return await renderWithPlugin(plugin, args);
  } finally {
    plugin.dispose();
  }
}

function cellSummary(cell) {
  const obj = cell.obj;
  return {
    ref: cell.transform.ref,
    transformer: cell.transform.transformer.id,
    status: cell.status,
    label: obj && obj.label ? obj.label : undefined,
    description: obj && obj.description ? obj.description : undefined,
    type: obj && obj.type ? `${obj.type.typeClass}:${obj.type.name}` : undefined,
  };
}

function safeProperty(fn, location) {
  try {
    const value = fn(location);
    return value === undefined || value === null ? '' : String(value);
  } catch {
    return '';
  }
}

function round(value) {
  if (!Number.isFinite(value)) return value;
  return Math.round(value * 1000) / 1000;
}

function vec3(value) {
  if (!value) return undefined;
  return [round(value[0]), round(value[1]), round(value[2])];
}

function compactObject(value) {
  const result = {};
  for (const [key, entry] of Object.entries(value)) {
    if (entry === undefined || entry === null || entry === '') continue;
    if (Array.isArray(entry) && entry.length === 0) continue;
    result[key] = entry;
  }
  return result;
}

function modelSummary(model) {
  if (!model) return {};
  const sourceData = model.sourceData || {};
  return compactObject({
    id: model.id,
    label: model.label,
    entry_id: model.entryId,
    model_num: model.modelNum,
    source_data_kind: sourceData.kind,
    source_data_name: sourceData.name,
    entities: model.entities && model.entities.data ? model.entities.data._rowCount : undefined,
    chains: model.atomicHierarchy && model.atomicHierarchy.chains ? model.atomicHierarchy.chains._rowCount : undefined,
    residues: model.atomicHierarchy && model.atomicHierarchy.residues ? model.atomicHierarchy.residues._rowCount : undefined,
    atoms: model.atomicHierarchy && model.atomicHierarchy.atoms ? model.atomicHierarchy.atoms._rowCount : undefined,
  });
}

function summarizeStructure(structure, obj, cell) {
  const chains = new Set();
  const residues = new Set();
  const elements = {};
  const loc = StructureElement.Location.create(structure);
  for (const unit of structure.units) {
    loc.unit = unit;
    for (const element of unit.elements) {
      loc.element = element;
      const chain = safeProperty(StructureProperties.chain.label_asym_id, loc);
      const authChain = safeProperty(StructureProperties.chain.auth_asym_id, loc);
      const comp = safeProperty(StructureProperties.atom.label_comp_id, loc);
      const seq = safeProperty(StructureProperties.residue.label_seq_id, loc);
      const model = safeProperty(StructureProperties.unit.model_num, loc);
      const symbol = safeProperty(StructureProperties.atom.type_symbol, loc);
      if (chain) chains.add(chain);
      residues.add(`${model}:${chain || authChain}:${seq}:${comp}`);
      if (symbol) elements[symbol] = (elements[symbol] || 0) + 1;
    }
  }
  const boundary = structure.boundary;
  return {
    label: obj && obj.label ? obj.label : undefined,
    description: obj && obj.description ? obj.description : undefined,
    state_ref: cell ? cell.transform.ref : undefined,
    atoms: structure.elementCount,
    residues: residues.size,
    chains: Array.from(chains).sort(),
    units: structure.units.length,
    models: structure.models.length,
    model_details: structure.models.map(modelSummary),
    polymer_residues: structure.polymerResidueCount,
    atomic_residues: structure.atomicResidueCount,
    elements,
    bounding_box: boundary && boundary.box ? { min: vec3(boundary.box.min), max: vec3(boundary.box.max) } : undefined,
    bounding_sphere: boundary && boundary.sphere ? { center: vec3(boundary.sphere.center), radius: round(boundary.sphere.radius) } : undefined,
  };
}

function inspectStructureCells(plugin) {
  const structures = [];
  for (const cell of plugin.state.data.cells.values()) {
    if (PluginStateObject.Molecule.Structure.is(cell.obj)) {
      structures.push(summarizeStructure(cell.obj.data, cell.obj, cell));
    }
  }
  return structures;
}

function transformForRef(plugin, ref) {
  if (!ref) return undefined;
  const transforms = plugin.state && plugin.state.data && plugin.state.data.tree && plugin.state.data.tree.transforms;
  if (!transforms) return undefined;
  if (typeof transforms.get === 'function') return transforms.get(ref);
  return transforms[ref];
}

function transformTags(transform) {
  const tags = transform && transform.tags;
  if (!tags) return [];
  if (Array.isArray(tags)) return tags;
  return [tags];
}

function mvsRefsFromTransform(transform) {
  return transformTags(transform)
    .filter((tag) => typeof tag === 'string' && tag.startsWith('mvs-ref:'))
    .map((tag) => tag.substring(8));
}

function mvsRefAncestry(plugin, cell) {
  const ancestry = [];
  const seen = new Set();
  let ref = cell && cell.transform ? cell.transform.ref : undefined;
  let distance = 0;
  while (ref && !seen.has(ref)) {
    seen.add(ref);
    const transform = transformForRef(plugin, ref) || (cell && cell.transform && cell.transform.ref === ref ? cell.transform : undefined);
    for (const mvsRef of mvsRefsFromTransform(transform)) {
      ancestry.push({ ref: mvsRef, state_ref: ref, distance, self: distance === 0 });
    }
    ref = transform && transform.parent;
    distance += 1;
  }
  return ancestry;
}

function paramValues(cell) {
  const params = cell && cell.transform ? cell.transform.params : undefined;
  if (!params) return {};
  if (params.values) return params.values;
  return params;
}

function namedParam(value) {
  if (value === undefined || value === null) return undefined;
  if (typeof value === 'string') return value;
  if (typeof value.name === 'string') return value.name;
  if (typeof value.type === 'string') return value.type;
  return undefined;
}

function representationSummary(plugin, cell) {
  const params = paramValues(cell);
  const ancestry = mvsRefAncestry(plugin, cell);
  const allRefs = Array.from(new Set(ancestry.map((entry) => entry.ref)));
  const ownerDistance = ancestry.find((entry) => !entry.self)?.distance;
  const componentRefs = Array.from(
    new Set(ancestry.filter((entry) => !entry.self && entry.distance === ownerDistance).map((entry) => entry.ref)),
  );
  const fallbackComponentRefs = componentRefs.length > 0 ? componentRefs : allRefs;
  const obj = cell.obj;
  return compactObject({
    state_ref: cell.transform.ref,
    parent_ref: cell.transform.parent,
    transformer: cell.transform.transformer.id,
    label: obj && obj.label ? obj.label : undefined,
    description: obj && obj.description ? obj.description : undefined,
    object_type: obj && obj.type ? `${obj.type.typeClass}:${obj.type.name}` : undefined,
    representation_type: namedParam(params.type),
    color_theme: namedParam(params.colorTheme),
    size_theme: namedParam(params.sizeTheme),
    mvs_refs: allRefs,
    component_refs: fallbackComponentRefs,
    mvs_ref_ancestry: ancestry,
  });
}

function inspectRepresentations(plugin) {
  const representations = [];
  for (const cell of plugin.state.data.cells.values()) {
    if (PluginStateObject.isRepresentation3D && PluginStateObject.isRepresentation3D(cell.obj)) {
      representations.push(representationSummary(plugin, cell));
    }
  }
  representations.sort((a, b) => String(a.state_ref).localeCompare(String(b.state_ref)));
  return representations;
}

function componentRepresentationMap(representations) {
  const mapping = {};
  for (const representation of representations) {
    const refs = representation.component_refs || [];
    for (const ref of refs) {
      if (!mapping[ref]) mapping[ref] = [];
      mapping[ref].push(representation);
    }
  }
  return mapping;
}

function cameraSummary(plugin) {
  const camera = plugin.canvas3d && plugin.canvas3d.camera;
  const state = camera && camera.state;
  if (!state) return {};
  return compactObject({
    position: vec3(state.position),
    target: vec3(state.target),
    up: vec3(state.up),
    radius: round(state.radius),
    radius_max: round(state.radiusMax),
    fog: round(state.fog),
    fov: round(state.fov),
    mode: state.mode,
  });
}

function inspectRefs(plugin) {
  const refs = [];
  const refMap = createMVSRefMap(plugin);
  for (const [ref, selectors] of Array.from(refMap.entries()).sort(([a], [b]) => a.localeCompare(b))) {
    const cells = selectors.map((selector) => selector.cell).filter(Boolean);
    refs.push({
      ref,
      cells: cells.map(cellSummary),
      structures: cells
        .filter((cell) => PluginStateObject.Molecule.Structure.is(cell.obj))
        .map((cell) => summarizeStructure(cell.obj.data, cell.obj, cell)),
    });
  }
  return refs;
}

function stateSummary(plugin) {
  const cells = Array.from(plugin.state.data.cells.values());
  const byStatus = {};
  const byType = {};
  for (const cell of cells) {
    byStatus[cell.status] = (byStatus[cell.status] || 0) + 1;
    const obj = cell.obj;
    const type = obj && obj.type ? `${obj.type.typeClass}:${obj.type.name}` : 'unknown';
    byType[type] = (byType[type] || 0) + 1;
  }
  return {
    cells: cells.length,
    by_status: byStatus,
    by_type: byType,
    bad_cells: collectBadCells(plugin),
  };
}

async function inspectWithPlugin(plugin, args) {
  try {
    await loadScene(plugin, args.input, args.noExtensions);
    const state = stateSummary(plugin);
    if (state.bad_cells.length > 0) {
      throw new Error(`building Mol* state failed: ${onelinerJsonString(state.bad_cells[0])}`);
    }
    const representations = inspectRepresentations(plugin);
    return {
      ok: true,
      input: args.input,
      state,
      camera: cameraSummary(plugin),
      refs: inspectRefs(plugin),
      structures: inspectStructureCells(plugin),
      representations,
      component_representations: componentRepresentationMap(representations),
    };
  } finally {
    await plugin.clear();
  }
}

async function inspectOnce(args) {
  const plugin = await createPlugin({ width: 64, height: 64 });
  try {
    return await inspectWithPlugin(plugin, args);
  } finally {
    plugin.dispose();
  }
}

let workerPlugin = null;
let workerPluginSize = '';

async function renderWorkerJob(args) {
  const sizeKey = `${args.size.width}x${args.size.height}`;
  if (!workerPlugin || workerPluginSize !== sizeKey) {
    if (workerPlugin) {
      workerPlugin.dispose();
      workerPlugin = null;
    }
    workerPlugin = await createPlugin(args.size);
    workerPluginSize = sizeKey;
  }
  return renderWithPlugin(workerPlugin, args);
}

async function disposeWorkerPlugin() {
  if (workerPlugin) {
    await workerPlugin.clear();
    workerPlugin.dispose();
    workerPlugin = null;
    workerPluginSize = '';
  }
}

function argsFromWorkerParams(params) {
  return {
    input: String(params.input || ''),
    output: String(params.output || ''),
    size: {
      width: Number(params.width || (params.size && params.size.width) || 800),
      height: Number(params.height || (params.size && params.size.height) || 800),
    },
    molj: Boolean(params.molj),
    noExtensions: Boolean(params.noExtensions),
    format: params.format ? normalizeFormat(params.format) : undefined,
    jpegQuality: params.jpegQuality ? parseInteger(params.jpegQuality, 'jpegQuality') : 90,
    quiet: true,
    json: false,
  };
}

function inspectArgsFromWorkerParams(params) {
  return {
    input: String(params.input || ''),
    noExtensions: Boolean(params.noExtensions),
    quiet: true,
    json: false,
  };
}

async function runWorker() {
  const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
  process.stderr.write(JSON.stringify({ event: 'ready', capabilities: rendererCapabilities() }) + '\n');
  for await (const line of rl) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    let request;
    try {
      request = JSON.parse(trimmed);
    } catch (error) {
      process.stdout.write(JSON.stringify({ ok: false, error: { message: `invalid JSON request: ${error.message}` } }) + '\n');
      continue;
    }
    const id = request.id;
    try {
      if (request.method === 'capabilities') {
        process.stdout.write(JSON.stringify({ id, ok: true, result: rendererCapabilities() }) + '\n');
        continue;
      }
      if (request.method === 'shutdown') {
        process.stdout.write(JSON.stringify({ id, ok: true, result: { shutdown: true } }) + '\n');
        break;
      }
      if (request.method === 'inspect') {
        const result = await inspectWithPlugin(await ensureInspectWorkerPlugin(), inspectArgsFromWorkerParams(request.params || {}));
        process.stdout.write(JSON.stringify({ id, ok: true, result }) + '\n');
        continue;
      }
      if (request.method !== 'render') {
        throw new Error(`unknown worker method: ${request.method}`);
      }
      const result = await renderWorkerJob(argsFromWorkerParams(request.params || {}));
      process.stdout.write(JSON.stringify({ id, ok: true, result }) + '\n');
    } catch (error) {
      process.stdout.write(JSON.stringify({ id, ok: false, error: { message: error.message || String(error), stack: error.stack || undefined } }) + '\n');
    }
  }
  await disposeWorkerPlugin();
}

async function ensureInspectWorkerPlugin() {
  if (!workerPlugin) {
    workerPlugin = await createPlugin({ width: 64, height: 64 });
    workerPluginSize = '64x64';
  }
  return workerPlugin;
}

async function main() {
  if (process.argv.includes('--worker')) {
    await runWorker();
    return;
  }
  if (process.argv.includes('--capabilities')) {
    console.log(JSON.stringify(rendererCapabilities(), null, 2));
    return;
  }
  if (process.argv.includes('--inspect')) {
    const args = parseInspectArgs(process.argv.slice(2));
    if (!args.quiet) console.error(`Inspecting ${args.input}`);
    const result = await inspectOnce(args);
    if (args.json) {
      console.log(JSON.stringify(result, null, 2));
    } else {
      console.log(JSON.stringify(result, null, 2));
    }
    return;
  }
  const args = parseArgs(process.argv.slice(2));
  if (!args.quiet) console.error(`Processing ${args.input} -> ${args.output}`);
  const result = await renderOnce(args);
  if (args.json) {
    console.log(JSON.stringify(result, null, 2));
  }
}

main().catch(async (error) => {
  await disposeWorkerPlugin();
  const expectedRendererError = error && error.code === 'HEADLESS_WEBGL_UNAVAILABLE';
  const message = expectedRendererError ? error.message : error && error.stack ? error.stack : String(error);
  if (process.argv.includes('--json')) {
    console.log(JSON.stringify({ ok: false, error: { message: error.message || String(error) } }, null, 2));
  }
  console.error(message);
  process.exit(1);
});
