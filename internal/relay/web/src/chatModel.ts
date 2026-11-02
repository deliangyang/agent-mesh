export function modelFitsEngine(model: string, engine: string, node: string): boolean {
  if (!engine) return true;
  if (!model) return false;
  const parts = model.split("/");
  if (parts[0] === engine) return true;
  if (node && parts[0] === node && parts[1] === engine) return true;
  return false;
}

export function chatModelOf(model: string, node: string, engine: string): string {
  if (modelFitsEngine(model, engine, node)) return model;
  if (model && engine && !model.includes("/")) {
    return node ? node + "/" + engine + "/" + model : engine + "/" + model;
  }
  if (node && engine) return node + "/" + engine;
  return engine || model;
}
