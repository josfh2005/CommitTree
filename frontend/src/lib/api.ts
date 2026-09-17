import * as Go from '../../wailsjs/go/app/App'
import type { Details, Filters, LogPage, Refs, Repo } from './types'

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
}
