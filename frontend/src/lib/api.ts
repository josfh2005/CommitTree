import * as Go from '../../wailsjs/go/app/App'
import type { AIMessage, AISettings, AIStatus, ConflictFile, Details, Filters, LogPage, MergeResult, MergeState, PromptInfo, Refs, Repo } from './types'

// The generated bindings use Wails model classes; the JSON is identical to our
// interfaces, so cast at this single boundary.
const call = <T>(p: Promise<unknown>) => p as Promise<T>

export const api = {
  listRepos: () => call<Repo[]>(Go.ListRepos()),
  addRepo: () => call<Repo>(Go.AddRepo()),
  relocateRepo: (id: string) => call<Repo>(Go.RelocateRepo(id)),
  removeRepo: (id: string) => call<void>(Go.RemoveRepo(id)),

  getRefs: (id: string) => call<Refs>(Go.GetRefs(id)),
  getLog: (id: string, filters: Filters, offset: number, limit: number) =>
    call<LogPage>(Go.GetLog(id, filters as any, offset, limit)),
  getDetails: (id: string, hash: string) => call<Details>(Go.GetDetails(id, hash)),
  getDiff: (id: string, parent: string, hash: string, paths: string[]) => call<string>(Go.GetDiff(id, parent, hash, paths)),
  getAuthors: (id: string) => call<string[]>(Go.GetAuthors(id)),
  resolveCommit: (id: string, text: string) => call<string>(Go.ResolveCommit(id, text)),
  isShallow: (id: string) => call<boolean>(Go.IsShallow(id)),
  fingerprint: (id: string) => call<string>(Go.Fingerprint(id)),

  checkout: (id: string, branch: string) => call<void>(Go.Checkout(id, branch)),
  checkoutRemote: (id: string, remote: string, name: string) => call<void>(Go.CheckoutRemote(id, remote, name)),
  checkoutDetached: (id: string, hash: string) => call<void>(Go.CheckoutDetached(id, hash)),
  fetch: (id: string) => call<void>(Go.Fetch(id)),
  pull: (id: string) => call<void>(Go.Pull(id)),
  createBranch: (id: string, name: string, target: string, checkout: boolean) =>
    call<void>(Go.CreateBranch(id, name, target, checkout)),
  deleteBranch: (id: string, name: string, force: boolean) => call<void>(Go.DeleteBranch(id, name, force)),
  deleteRemoteBranch: (id: string, remote: string, name: string) => call<void>(Go.DeleteRemoteBranch(id, remote, name)),
  createTag: (id: string, name: string, target: string, message: string) => call<void>(Go.CreateTag(id, name, target, message)),
  deleteTag: (id: string, name: string) => call<void>(Go.DeleteTag(id, name)),

  mergeBranch: (id: string, branch: string) => call<MergeResult>(Go.MergeBranch(id, branch)),
  getMergeState: (id: string) => call<MergeState>(Go.GetMergeState(id)),
  abortMerge: (id: string) => call<void>(Go.AbortMerge(id)),
  commitMerge: (id: string) => call<void>(Go.CommitMerge(id)),
  resolveConflicts: (repoID: string, runID: string) => call<void>(Go.ResolveConflicts(repoID, runID)),
  getConflictFile: (id: string, path: string) => call<ConflictFile>(Go.GetConflictFile(id, path)),

  aiStatus: () => call<AIStatus>(Go.AIStatus()),
  getAISettings: () => call<AISettings>(Go.GetAISettings()),
  saveAISettings: (s: AISettings) => call<void>(Go.SaveAISettings(s as any)),
  pullModel: (name: string) => call<void>(Go.PullModel(name)),
  cancelPull: () => call<void>(Go.CancelPull()),
  getChat: (repoID: string) => call<AIMessage[]>(Go.GetChat(repoID)),
  sendChat: (repoID: string, text: string, runID: string) => call<void>(Go.SendChat(repoID, text, runID)),
  stopChat: (repoID: string) => call<void>(Go.StopChat(repoID)),
  clearChat: (repoID: string) => call<void>(Go.ClearChat(repoID)),
  explainInChat: (repoID: string, hash: string, provider: '' | 'apple' | 'ollama', runID: string) =>
    call<void>(Go.ExplainInChat(repoID, hash, provider, runID)),
  listPrompts: () => call<PromptInfo[]>(Go.ListPrompts()),
  openPromptsFolder: () => call<void>(Go.OpenPromptsFolder()),
  resetPrompt: (name: string) => call<void>(Go.ResetPrompt(name)),
}
