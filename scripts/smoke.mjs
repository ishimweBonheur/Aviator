// Local SANDBOX smoke check. Creates a uniquely named test account and ledger.
// Run with the backend active: node scripts/smoke.mjs
import assert from 'node:assert/strict';
const base=process.env.SMOKE_URL??'http://localhost:7000';
let token='';
async function api(path,body,expected=200){
 const r=await fetch(base+path,{method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json',...(token?{Authorization:'Bearer '+token}:{})},body:body===undefined?undefined:JSON.stringify(body)});
 const data=await r.json();assert.equal(r.status,expected,JSON.stringify(data));return data;
}
const delay=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,timeout=180000){const end=Date.now()+timeout;while(Date.now()<end){const value=await fn();if(value)return value;await delay(100)}throw Error('timed out waiting for round');}
const name='smoke'+Date.now(),password='LocalSandbox123!';
await api('/api/auth/register',{username:name,email:name+'@example.test',password},201);
token=(await api('/api/auth/login',{email:name+'@example.test',password})).token;
const deposit=await api('/api/deposits',{amount:'10000.00',provider:'SANDBOX'},201);
assert.equal(deposit.status,'COMPLETED');assert.equal(Number((await api('/api/wallet/balance')).balance),10000);
assert.equal((await api('/api/deposits',{amount:'100.00',provider:'MTN_MOMO'},201)).status,'PENDING');
assert.equal(Number((await api('/api/wallet/balance')).balance),10000);
const events=new Set();const ws=new WebSocket(base.replace('http','ws')+'/ws');
ws.onmessage=e=>events.add(JSON.parse(e.data).type);
try{
 const round=await until(async()=>{const s=await api('/api/game/rounds/current');return s.upcoming?.status==='BETTING_OPEN'&&Date.parse(s.upcoming.betting_closes_at)-Date.parse(s.server_time)>2000?s.upcoming:null});
 assert.equal(round.server_seed,undefined);assert.equal(round.crash_point,undefined);
 const bet=(await api('/api/bets',{round_id:round.id,bet_number:1,amount:'100.00'},201)).bet;
 assert.equal(Number((await api('/api/wallet/balance')).balance),9900);
 await api('/api/bets/'+bet.id+'/cancel',{});await api('/api/bets/'+bet.id+'/cancel',{},409);
 assert.equal(Number((await api('/api/wallet/balance')).balance),10000);
 const bet2=(await api('/api/bets',{round_id:round.id,bet_number:2,amount:'100.00'},201)).bet;
 await until(async()=>{const r=await api('/api/game/rounds/'+round.id);return r.status==='RUNNING'||r.status==='SETTLED'});
 const cash=await fetch(base+'/api/bets/'+bet2.id+'/cashout',{method:'POST',headers:{Authorization:'Bearer '+token}});
 const result=await cash.json();
 if(cash.ok){assert.match(result.multiplier,/^\d+\.\d{2}$/);await api('/api/bets/'+bet2.id+'/cashout',{},409);console.log('cashout and duplicate rejection passed')}
 else {assert.equal(cash.status,409);console.log('instant crash correctly rejected cashout')}
 await until(async()=>{const r=await api('/api/game/rounds/'+round.id);return r.status==='SETTLED'});
 await api('/api/bets/'+bet2.id+'/cashout',{},409);
 const fairness=await api('/api/game/rounds/'+round.id+'/fairness');assert.ok(fairness.server_seed);assert.ok(fairness.crash_point);
 const before=Number((await api('/api/wallet/balance')).balance);
 const withdrawal=await api('/api/withdrawals',{amount:'50.00',provider:'SANDBOX'},201);assert.equal(withdrawal.withdrawal.status,'PENDING');
 assert.equal(Number((await api('/api/wallet/balance')).balance),before-50);
 assert.ok((await api('/api/wallet/transactions')).length>=5);
 assert.ok((await api('/api/bets')).every(b=>b.user_id===bet.user_id));
 assert.ok(events.has('MULTIPLIER_UPDATE'));assert.ok(events.has('ROUND_SETTLED'));
 const control=await fetch(base+'/api/game/rounds/'+round.id+'/start',{method:'POST'});assert.ok(control.status>=400);
 console.log('PASS: auth, SANDBOX deposit, MTN pending, balance, betting, refund, duplicate rejection, settlement, fairness, withdrawal, history, WebSocket and lifecycle route protection');
}finally{ws.close()}
