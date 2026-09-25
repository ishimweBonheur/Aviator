// Requires built aviator-local.exe, .env, PostgreSQL and this project's Redis.
// Stop other local backends first. Exercises actual process restart/failover.
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import assert from 'node:assert/strict';
const delay=ms=>new Promise(r=>setTimeout(r,ms));
const base='http://localhost:7000';
let child;
async function api(path){return (await fetch(base+path)).json()}
async function until(fn,timeout=180000){const end=Date.now()+timeout;while(Date.now()<end){try{const v=await fn();if(v)return v}catch{}await delay(100)}throw Error('recovery wait timed out')}
async function start(){child=spawn('./aviator-local.exe',[],{windowsHide:true,stdio:'ignore',env:{...process.env,REDIS_ADDR:'localhost:6380'}});await until(async()=>{const r=await fetch(base+'/health');return r.ok},20000)}
async function stop(){if(child&&child.exitCode===null){child.kill();await once(child,'exit')}}
try{
 await start();
 const open=await until(async()=>{const s=await api('/api/game/rounds/current');return s.upcoming?.status==='BETTING_OPEN'?s.upcoming:null});
 await stop();await delay(7000);await start();
 const resumed=await until(async()=>{const r=await api('/api/game/rounds/'+open.id);return r.status!=='BETTING_OPEN'?r:null});
 assert.equal(resumed.betting_closes_at,open.betting_closes_at);
 console.log('PASS: expired betting deadline preserved across process restart');
 const running=await until(async()=>{const s=await api('/api/game/rounds/current');return s.running});
 await stop();await delay(11000);await start();
 const after=await api('/api/game/rounds/'+running.id);assert.equal(after.started_at,running.started_at);
 await until(async()=>{const r=await api('/api/game/rounds/'+running.id);return r.status==='SETTLED'});
 console.log('PASS: original flight timing preserved and recovered round settled');
 const follower=spawn('./aviator-local.exe',[],{windowsHide:true,stdio:'ignore',env:{...process.env,APP_PORT:'7001',REDIS_ADDR:'localhost:6380'}});
 try{
  await until(async()=>(await fetch('http://localhost:7001/health')).ok,20000);
  const round=await until(async()=>{const s=await api('/api/game/rounds/current');return s.running});
  const other=await (await fetch('http://localhost:7001/api/game/rounds/'+round.id)).json();assert.equal(other.started_at,round.started_at);
  console.log('PASS: follower serves the same authoritative round without creating another engine');
 }finally{follower.kill();await once(follower,'exit')}
}finally{await stop()}
