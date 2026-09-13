import assert from 'node:assert/strict';
import test from 'node:test';
import { belongsToPR, gate, inspectPR, reconcileRuns, run, syncLabels } from './contribution-governance.mjs';
import { noHumanQA, bodyFingerprint } from './contribution-policy.mjs';

const body = `## Author\n- [x] Agent\n## Type\n- [x] bug\n## Summary\nFix the reported issue\n## Validation\nRegression tests passed\n## Screenshots / Recordings\nBackend-only change without a UI; verified using API requests\n## Human QA\n- [x] Not yet verified by a human\n\n${noHumanQA}`;
const pr = { number:1,state:'open',head:{sha:'abc',repo:{id:2},ref:'patch'},base:{ref:'main',repo:{id:1}},body,labels:[],user:{login:'author'},changed_files:1,additions:10,deletions:0 };
const ci = {id:10,path:'.github/workflows/eslint.yml',head_sha:'abc',event:'pull_request',run_attempt:1,pull_requests:[{number:1,head:{sha:'abc'},base:{repo:{id:1}}}]};
function mock({ runs=[], jobs=[], fresh=pr, statuses=[], files=[{filename:'apps/web/a.vue',additions:10,deletions:0}], comments=[] } = {}) {
  const calls=[];
  const rest={actions:{},pulls:{},repos:{},issues:{}};
  for (const [section,names] of Object.entries({actions:['approveWorkflowRun','cancelWorkflowRun','reRunWorkflow'],repos:['createCommitStatus'],issues:['addLabels','removeLabel','createComment','updateComment']})) {
    for (const name of names) rest[section][name]=async args=>{calls.push({name,args});return {data:{}};};
  }
  rest.pulls.get=async()=>({data:fresh});
  for (const [section,names] of Object.entries({actions:['listWorkflowRunsForRepo','listJobsForWorkflowRun'],repos:['listCommitStatusesForRef'],pulls:['listFiles'],issues:['listComments']})) for (const name of names) rest[section][name]=name;
  const values={listWorkflowRunsForRepo:runs,listJobsForWorkflowRun:jobs,listCommitStatusesForRef:statuses,listFiles:files,listComments:comments};
  const github={rest,paginate:async method=>values[method]};
  const summary={addHeading(){return this;},addRaw(){return this;},async write(){}};
  return {github,calls,core:{info(){},summary},context:{repo:{owner:'o',repo:'r'},serverUrl:'https://github.com',runId:99}};
}
test('approval binds the repository, PR, workflow path and current head', () => {
  assert.equal(belongsToPR(ci,pr),true);
  for (const changed of [{...ci,head_sha:'old'},{...ci,event:'push'},{...ci,path:'.github/workflows/release.yml'},{...ci,pull_requests:[{number:99,head:{sha:'abc'},base:{repo:{id:1}}}]}]) assert.equal(belongsToPR(changed,pr),false);
  assert.equal(belongsToPR({...ci,pull_requests:[],head_repository:{id:2},head_branch:'patch'},pr),true);
  assert.equal(belongsToPR({...ci,pull_requests:[],head_repository:{id:3},head_branch:'patch'},pr),false);
});
test('valid PR approves pending CI; obsolete events and invalid bodies cannot approve', async () => {
  for (const [valid,fresh,count] of [[true,pr,1],[false,pr,0],[true,{...pr,body:'changed'},0]]) {
    const m=mock({runs:[{...ci,status:'completed',conclusion:'action_required'}],fresh});
    await reconcileRuns(m.github,m.context.repo,pr,valid,m.core);
    assert.equal(m.calls.filter(c=>c.name==='approveWorkflowRun').length,count);
  }
});
test('only latest run is approved, never obsolete runs of the same workflow', async () => {
  const m=mock({runs:[{...ci,id:9,conclusion:'action_required'},{...ci,id:10,status:'in_progress'}]});
  await reconcileRuns(m.github,m.context.repo,pr,true,m.core);
  assert.equal(m.calls.length,0);
});
test('rerun only format failures, never actual test failures', async () => {
  for (const [jobs,count] of [
    [[{name:'format / Contribution format',conclusion:'failure'},{name:'Test',conclusion:'skipped'}],1],
    [[{name:'format / Contribution format',conclusion:'success'},{name:'Test',conclusion:'failure'}],0],
    [[{name:'format / Contribution format',conclusion:'failure'},{name:'Test',conclusion:'failure'}],0],
  ]) {
    const m=mock({runs:[{...ci,status:'completed',conclusion:'failure'}],jobs});
    await reconcileRuns(m.github,m.context.repo,pr,true,m.core);
    assert.equal(m.calls.filter(c=>c.name==='reRunWorkflow').length,count);
  }
});
test('invalid PR cancels running CI with an attempt-bound recovery marker',async()=>{
  const m=mock({runs:[{...ci,status:'in_progress'}]});
  await reconcileRuns(m.github,m.context.repo,pr,false,m.core);
  assert.deepEqual(m.calls.map(c=>c.name),['createCommitStatus','cancelWorkflowRun']);
  assert.equal(m.calls[0].args.description,'attempt:1');
});
test('only controller cancellations from the same attempt are recoverable',async()=>{
  for(const [statuses,count] of [[[],0],[[{context:'PR Format cancellation / 10',description:'attempt:1',creator:{login:'github-actions[bot]'}}],1],[[{context:'PR Format cancellation / 10',description:'attempt:0',creator:{login:'github-actions[bot]'}}],0]]) {
    const m=mock({runs:[{...ci,status:'completed',conclusion:'cancelled'}],statuses});
    await reconcileRuns(m.github,m.context.repo,pr,true,m.core);
    assert.equal(m.calls.filter(c=>c.name==='reRunWorkflow').length,count);
  }
});
test('synchronization removes obsolete managed labels and preserves unrelated labels',async()=>{
  const m=mock();
  await syncLabels(m.github,m.context.repo,{number:1,labels:[{name:'size:XS'},{name:'custom'}]},['size:M'],name=>name.startsWith('size:'));
  assert.deepEqual(m.calls.map(c=>c.name),['addLabels','removeLabel']);
  assert.equal(m.calls[1].args.name,'size:XS');
});
test('classification failure does not invent a format error or remove prior scope labels',async()=>{
  const m=mock({files:[],fresh:{...pr,labels:[{name:'size:L'}]}});
  await assert.rejects(inspectPR(m,1),/Incomplete/);
  assert.equal(m.calls.find(c=>c.name==='createCommitStatus').args.state,'success');
  assert.ok(!m.calls.some(c=>c.name==='removeLabel' && c.args.name==='size:L'));
  assert.ok(!m.calls.some(c=>c.name==='addLabels' && c.args.labels.includes('needs:format')));
});
test('fixed descriptions update the existing bot comment instead of posting another',async()=>{
  const m=mock({comments:[{id:5,user:{login:'github-actions[bot]'},body:'<!-- memoh-contribution-format:v1 -->\nPrevious error'}]});
  await inspectPR(m,1);
  assert.equal(m.calls.filter(c=>c.name==='updateComment').length,1);
  assert.equal(m.calls.filter(c=>c.name==='createComment').length,0);
});
test('unchanged success is idempotent',async()=>{
  const m=mock({fresh:{...pr,labels:[{name:'bug'},{name:'size:XS'},{name:'change:web'}]},statuses:[{context:'PR Format',state:'success',description:`${bodyFingerprint(pr)} Description format passed`}]});
  await inspectPR(m,1);
  assert.deepEqual(m.calls,[]);
});
test('issues use current API body, not stale event body',async()=>{
  const m=mock();
  m.context.eventName='issues';m.context.payload={issue:{number:1,body:'stale'}};
  m.github.rest.issues.get=async()=>({data:{number:1,state:'open',body:'',labels:[],user:{login:'person'}}});
  await run(m);
  assert.ok(m.calls.find(c=>c.name==='createComment').args.body.includes('@person'));
  assert.ok(m.calls.find(c=>c.name==='addLabels').args.labels.includes('needs:format'));
});

test('read-only CI accepts a matching trusted controller status', async () => {
  const m=mock({statuses:[{context:'PR Format',state:'success',description:`${bodyFingerprint(pr)} Description format passed`,creator:{login:'github-actions[bot]'}}]});
  m.context.payload={pull_request:pr};
  await gate(m);
  assert.deepEqual(m.calls,[]);
});
test('CI gate rejects obsolete head or invalid body before executing code jobs', async () => {
  for(const fresh of [{...pr,head:{...pr.head,sha:'new'}},{...pr,body:''}]) {
    const m=mock({fresh});m.context.payload={pull_request:pr};
    await assert.rejects(gate(m));
    assert.deepEqual(m.calls,[]);
  }
});

test('read-only gate rejects a description edited during validation', async () => {
  const m=mock({statuses:[{context:'PR Format',state:'success',description:`${bodyFingerprint(pr)} Description format passed`,creator:{login:'github-actions[bot]'}}]});m.context.payload={pull_request:pr};
  let reads=0;
  m.github.rest.pulls.get=async()=>({data:++reads===1?pr:{...pr,body:''}});
  await assert.rejects(gate(m),/PR changed/);
});

test('gate rejects missing, stale, failed, or untrusted controller statuses', async t => {
  t.mock.method(globalThis, 'setTimeout', callback => { queueMicrotask(callback); return 0; });
  const valid={context:'PR Format',state:'success',description:`${bodyFingerprint(pr)} Description format passed`,creator:{login:'github-actions[bot]'}};
  for(const statuses of [[],[{...valid,description:'stale fingerprint'}],[{...valid,state:'failure'}],[{...valid,creator:{login:'contributor'}}]]) {
    const m=mock({statuses});m.context.payload={pull_request:pr};
    await assert.rejects(gate(m),/Waiting for trusted PR Format status/);
  }
});
