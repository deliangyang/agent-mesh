export type MeshEvent = {
  type: string;
  role?: string;
  tool?: string;
  text?: string;
  preview?: string;
  unix?: number;
  done?: boolean;
};

export type NodeInfo = {
  id: string;
  name?: string;
  os?: string;
  arch?: string;
};

export type EngineRow = {
  name: string;
  title?: string;
  node_id?: string;
  available?: boolean;
  enabled?: boolean;
};

export type ModelCard = {
  id: string;
  owned_by?: string;
};

export type DirRow = {
  path: string;
  engine?: string;
  node_id?: string;
  node_name?: string;
};

export type Session = {
  id: string;
  engine: string;
  title?: string;
  cwd?: string;
  live?: boolean;
  busy?: boolean;
  updated_at?: number;
  node_id?: string;
  node_name?: string;
};

export type CatalogState = {
  nodes: NodeInfo[];
  engines: EngineRow[];
  models: ModelCard[];
  dirs: DirRow[];
  sessions: Session[];
};

export type MeshTarget = {
  node: string;
  engine: string;
  session: string;
  model: string;
  cwd: string;
};

export type SlashItem = {
  name: string;
  kind: string;
  description?: string;
  argument_hint?: string;
  source?: string;
};

export type AskOption = {
  id: string;
  label: string;
  hint?: string;
};

export type AskQuestion = {
  id?: string;
  prompt: string;
  header?: string;
  multi?: boolean;
  options: AskOption[];
};

export type AskPrompt = {
  id?: string;
  kind: string;
  title?: string;
  questions: AskQuestion[];
};

export type GitRepo = {
  path: string;
  name: string;
  branch?: string;
  dirty?: boolean;
  files?: number;
};

export type GitFile = {
  path: string;
  status: string;
  patch?: string;
};

export type GitBundle = {
  repo: GitRepo;
  files: GitFile[];
};

export type FollowItem = {
  id: string;
  text: string;
  created: number;
};

export type FollowQueue = {
  engine?: string;
  session?: string;
  cwd?: string;
  node_id?: string;
  items?: FollowItem[];
};
