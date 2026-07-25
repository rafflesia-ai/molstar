import { spawn } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

export type Provider = 'pdbe' | 'rcsb' | 'alphafold' | 'afdb';
export type RepresentationType = 'cartoon' | 'ball-and-stick' | 'spacefill' | 'line' | 'surface' | 'backbone' | string;
export type ColorSpec = 'chain' | 'element' | 'entity' | 'plddt' | 'confidence' | string;
export type Selector = 'all' | 'polymer' | 'protein' | 'nucleic' | 'ligand' | 'ion' | 'water' | string;
export type View = 'front' | 'back' | 'top' | 'bottom' | 'left' | 'right';

export interface RuntimeSpec {
  cache?: string;
  profile?: 'default' | 'ci' | 'locked';
  network?: boolean;
  offline?: boolean;
  strict?: boolean;
  timeout_seconds?: number;
  max_pixels?: number;
  max_atoms?: number;
  max_outputs?: number;
  max_download_bytes?: number;
  max_archive_bytes?: number;
  allow_hosts?: string[];
  allow_paths?: string[];
}

export interface InputSpec {
  id?: string;
  provider?: Provider;
  path?: string;
  url?: string;
  format?: string;
  assembly?: string;
}

export interface RepresentationSpec {
  type?: RepresentationType;
  color?: ColorSpec;
  size_factor?: number;
  ignore_hydrogens?: boolean;
}

export interface ComponentSpec {
  ref?: string;
  select: string;
  representation?: RepresentationSpec;
  label?: string;
  tooltip?: string;
}

export interface StructureSpec {
  ref?: string;
  source: string;
  type?: 'model' | 'assembly' | 'symmetry' | 'symmetry_mates';
  assembly?: string;
  components?: ComponentSpec[];
}

export interface JobSpec {
  version: 1;
  runtime?: RuntimeSpec;
  inputs: Record<string, InputSpec>;
  scene: {
    canvas?: { background?: string };
    structures: StructureSpec[];
    camera?: {
      focus?: string;
      view?: View;
      zoom?: number;
      target?: [number, number, number];
      position?: [number, number, number];
      up?: [number, number, number];
      direction?: [number, number, number];
      near?: number;
    };
  };
  outputs?: Array<{
    type: string;
    path: string;
    size?: [number, number];
    transparent?: boolean;
    quality?: 'low' | 'medium' | 'high';
  }>;
  assets?: Array<{
    name: string;
    path: string;
  }>;
}

export interface OutputReport {
  path: string;
  type: string;
  bytes?: number;
  sha256?: string;
  width?: number;
  height?: number;
  average_hash?: string;
  verified: boolean;
  non_blank?: boolean;
  atomic?: boolean;
}

export interface StageReport {
  name: string;
  detail?: string;
  ok: boolean;
  started_at: string;
  duration_ms?: number;
  error?: string;
}

export interface CLIReport {
  ok?: boolean;
  [key: string]: unknown;
}

export interface RenderReport extends CLIReport {
  input?: string;
  mvs?: string;
  outputs?: string[];
  output_files?: OutputReport[];
  warnings?: string[];
  themes?: Array<Record<string, unknown>>;
  commands?: Array<Record<string, unknown>>;
  cached_inputs?: Array<Record<string, unknown>>;
  diagnostics?: Record<string, unknown>;
  stages?: StageReport[];
}

export interface ExportReport extends CLIReport {
  output?: string;
  warnings?: string[];
  themes?: Array<Record<string, unknown>>;
  cached_inputs?: Array<Record<string, unknown>>;
}

export interface HeadlessMolstarOptions {
  binary?: string;
  size?: [number, number];
  cache?: string;
  runtime?: RuntimeSpec;
}

export class HeadlessMolstar {
  readonly binary: string;
  readonly size: [number, number];
  readonly runtime: RuntimeSpec;

  private constructor(options: HeadlessMolstarOptions = {}) {
    this.binary = options.binary ?? 'molstar';
    this.size = options.size ?? [800, 800];
    this.runtime = { ...(options.runtime ?? {}) };
    if (options.cache) this.runtime.cache = options.cache;
  }

  static async create(options: HeadlessMolstarOptions = {}): Promise<HeadlessMolstar> {
    return new HeadlessMolstar(options);
  }

  scene(): SceneBuilder {
    return new SceneBuilder(this.runtime);
  }

  render(scene: SceneBuilder | JobSpec): RenderBuilder {
    return new RenderBuilder(this.binary, normalizeScene(scene), this.size);
  }

  export(scene: SceneBuilder | JobSpec): ExportBuilder {
    return new ExportBuilder(this.binary, normalizeScene(scene));
  }

  async dispose(): Promise<void> {}
}

export class SceneBuilder {
  private readonly job: JobSpec;
  private currentStructure?: StructureSpec;

  constructor(runtime: RuntimeSpec = {}) {
    this.job = {
      version: 1,
      runtime: Object.keys(runtime).length > 0 ? { ...runtime } : undefined,
      inputs: {},
      scene: { structures: [], canvas: {} },
      outputs: [],
    };
  }

  load(input: InputSpec & { ref?: string }): this {
    const ref = input.ref ?? 'input';
    const { ref: _ref, ...stored } = input;
    this.job.inputs[ref] = stored;
    this.currentStructure = { ref, source: ref, components: [] };
    this.job.scene.structures.push(this.currentStructure);
    return this;
  }

  structure(options: Omit<Partial<StructureSpec>, 'source' | 'components'> = {}): this {
    this.ensureStructure();
    Object.assign(this.currentStructure!, options);
    return this;
  }

  component(select: Selector, ref = sanitizeRef(select)): ComponentBuilder {
    this.ensureStructure();
    const component: ComponentSpec = {
      ref,
      select,
      representation: {},
    };
    this.currentStructure!.components!.push(component);
    return new ComponentBuilder(this, component);
  }

  focus(target: string, options: { view?: View; zoom?: number } = {}): this {
    this.job.scene.camera = { ...(this.job.scene.camera ?? {}), focus: target, ...options };
    return this;
  }

  view(view: View): this {
    this.job.scene.camera = { ...(this.job.scene.camera ?? {}), view };
    return this;
  }

  canvas(options: { background?: string }): this {
    this.job.scene.canvas = { ...(this.job.scene.canvas ?? {}), ...options };
    return this;
  }

  output(output: NonNullable<JobSpec['outputs']>[number]): this {
    this.job.outputs = [...(this.job.outputs ?? []), output];
    return this;
  }

  asset(name: string, path: string): this {
    this.job.assets = [...(this.job.assets ?? []), { name, path }];
    return this;
  }

  toJob(): JobSpec {
    return JSON.parse(JSON.stringify(this.job)) as JobSpec;
  }

  private ensureStructure(): void {
    if (!this.currentStructure) this.load({ ref: 'input', id: '1cbs', provider: 'pdbe' });
  }
}

export class ComponentBuilder {
  constructor(private readonly sceneBuilder: SceneBuilder, private readonly componentSpec: ComponentSpec) {}

  repr(type: RepresentationType): this {
    this.componentSpec.representation = { ...(this.componentSpec.representation ?? {}), type };
    return this;
  }

  color(color: ColorSpec): SceneBuilder {
    this.componentSpec.representation = { ...(this.componentSpec.representation ?? {}), color };
    return this.sceneBuilder;
  }

  label(text: string): this {
    this.componentSpec.label = text;
    return this;
  }

  tooltip(text: string): this {
    this.componentSpec.tooltip = text;
    return this;
  }

  done(): SceneBuilder {
    return this.sceneBuilder;
  }

  component(select: Selector, ref?: string): ComponentBuilder {
    return this.sceneBuilder.component(select, ref);
  }

  focus(target: string, options?: { view?: View; zoom?: number }): SceneBuilder {
    return this.sceneBuilder.focus(target, options);
  }

  canvas(options: { background?: string }): SceneBuilder {
    return this.sceneBuilder.canvas(options);
  }
}

export class RenderBuilder {
  constructor(private readonly binary: string, private readonly job: JobSpec, private readonly defaultSize: [number, number]) {}

  png(path: string, size: [number, number] = this.defaultSize): Promise<RenderReport> {
    return this.runOutput({ type: 'image', path, size });
  }

  jpg(path: string, size: [number, number] = this.defaultSize): Promise<RenderReport> {
    return this.runOutput({ type: 'jpg', path, size });
  }

  mp4(path: string, size: [number, number] = this.defaultSize): Promise<RenderReport> {
    return this.runOutput({ type: 'video', path, size });
  }

  async pngWithReport(path: string, reportPath: string, size: [number, number] = this.defaultSize): Promise<RenderReport> {
    return this.runOutput({ type: 'image', path, size }, ['--report', reportPath]);
  }

  private async runOutput(output: NonNullable<JobSpec['outputs']>[number], extraArgs: string[] = []): Promise<RenderReport> {
    const job = { ...this.job, outputs: [output] };
    const dir = await mkdtemp(join(tmpdir(), 'headlessmolstar-'));
    try {
      const jobPath = join(dir, 'job.json');
      await writeFile(jobPath, JSON.stringify(job, null, 2));
      return await runCLIJSON<RenderReport>(this.binary, ['render', jobPath, '--json', ...extraArgs]);
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  }
}

export class ExportBuilder {
  constructor(private readonly binary: string, private readonly job: JobSpec) {}

  async mvsj(path: string): Promise<ExportReport> {
    const dir = await mkdtemp(join(tmpdir(), 'headlessmolstar-'));
    try {
      const jobPath = join(dir, 'job.json');
      await writeFile(jobPath, JSON.stringify(this.job, null, 2));
      return await runCLIJSON<ExportReport>(this.binary, ['scene', 'compile', jobPath, '--out', path, '--json']);
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  }

  async mvsx(path: string): Promise<ExportReport> {
    const dir = await mkdtemp(join(tmpdir(), 'headlessmolstar-'));
    try {
      const jobPath = join(dir, 'job.json');
      await writeFile(jobPath, JSON.stringify(this.job, null, 2));
      return await runCLIJSON<ExportReport>(this.binary, ['scene', 'compile', jobPath, '--out', path, '--json']);
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  }

  toJob(): JobSpec {
    return JSON.parse(JSON.stringify(this.job)) as JobSpec;
  }
}

function normalizeScene(scene: SceneBuilder | JobSpec): JobSpec {
  return scene instanceof SceneBuilder ? scene.toJob() : JSON.parse(JSON.stringify(scene));
}

interface CLIProcessResult {
  stdout: string;
  stderr: string;
}

function runCLI(binary: string, args: string[]): Promise<CLIProcessResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(binary, args, { stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk: Buffer) => {
      stdout += chunk.toString();
    });
    child.stderr.on('data', (chunk: Buffer) => {
      stderr += chunk.toString();
    });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        resolve({ stdout, stderr });
        return;
      }
      reject(new Error(stderr.trim() || stdout.trim() || `${binary} exited with ${code}`));
    });
  });
}

async function runCLIJSON<T extends CLIReport>(binary: string, args: string[]): Promise<T> {
  const result = await runCLI(binary, args);
  try {
    return JSON.parse(result.stdout) as T;
  } catch (error) {
    throw new Error(`failed to parse ${binary} JSON output: ${(error as Error).message}\nstdout:\n${result.stdout}\nstderr:\n${result.stderr}`);
  }
}

function sanitizeRef(value: string): string {
  const ref = value.trim().toLowerCase().replace(/[\s-]+/g, '_');
  return ref || 'component';
}
