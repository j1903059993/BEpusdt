// Build from the owner's committed checkout; never downloads an upstream release.
import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';
import {execFileSync} from 'node:child_process';

const root=process.cwd(), output=path.resolve(process.argv[2]||'');
const git=args=>execFileSync('git',args,{cwd:root,encoding:'utf8'}).trim();
const owner='j1903059993/BEpusdt';
if(!process.argv[2]||output===root||output.startsWith(root+path.sep))throw Error('Specify a new output directory outside the checkout');
if(git(['status','--porcelain']))throw Error('Commit gateway sources before building');
if(!/^https:\/\/github\.com\/j1903059993\/BEpusdt(?:\.git)?$/i.test(git(['remote','get-url','origin'])))throw Error('Owner-controlled source checkout required');
const commit=git(['rev-parse','HEAD']),version='v1.24.3-mantou-'+commit.slice(0,8);
const go=process.env.MANTOU_GO||'go';
const digest=async file=>crypto.createHash('sha256').update(await fs.readFile(file)).digest('hex');
await fs.mkdir(output);
const source=path.join(output,'source');await fs.mkdir(source);
execFileSync('git',['archive','--format=tar','-o',path.join(output,'tracked-source.tar'),commit],{cwd:root});
execFileSync('tar',['-xf',path.join(output,'tracked-source.tar'),'-C',source]);
// Build the owned admin UI using the committed lockfile before invoking this file.
await fs.access(path.join(root,'web/dist/secure.html'));
await fs.cp(path.join(root,'web/dist'),path.join(source,'static/secure'),{recursive:true});
const buildEnv={...process.env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'};
// Archive vendored dependencies and generated static assets with corresponding
// source, so backend recovery does not require fetching deleted Git repositories.
execFileSync(go,['mod','vendor'],{cwd:source,stdio:'inherit'});
const bundle=path.join(output,'runtime');await fs.mkdir(bundle);
const binary=path.join(bundle,'bepusdt');
execFileSync(go,['build','-mod=vendor','-trimpath','-ldflags',`-s -w -X github.com/v03413/bepusdt/app.Version=${version}`,'-o',binary,'./main'],{cwd:source,stdio:'inherit',env:buildEnv});
const sourceArchive=path.join(output,`bepusdt-source-${commit.slice(0,8)}.tar.gz`);
execFileSync('tar',['-czf',sourceArchive,'-C',source,'.']);
const meta={repository:owner,commit,version,target:'linux/amd64',binarySha256:await digest(binary),sourceArchive:path.basename(sourceArchive),sourceSha256:await digest(sourceArchive),go:execFileSync(go,['version'],{encoding:'utf8'}).trim(),eventProof:1};
await fs.copyFile(path.join(root,'LICENSE'),path.join(bundle,'LICENSE'));
await fs.writeFile(path.join(bundle,'gateway-release.json'),JSON.stringify(meta,null,2)+'\n');
const rows=[];for(const name of await fs.readdir(bundle))rows.push(await digest(path.join(bundle,name))+'  '+name);
await fs.writeFile(path.join(bundle,'manifest.sha256'),rows.sort().join('\n')+'\n');
const runtimeArchive=path.join(output,`bepusdt-runtime-${commit.slice(0,8)}.tar.gz`);
execFileSync('tar',['-czf',runtimeArchive,'-C',bundle,'.']);
const hashes=[sourceArchive,runtimeArchive];
await fs.writeFile(path.join(output,'SHA256SUMS'),(await Promise.all(hashes.map(async f=>await digest(f)+'  '+path.basename(f)))).join('\n')+'\n');
console.log(JSON.stringify({...meta,runtimeArchive,runtimeSha256:await digest(runtimeArchive),runtimeBytes:(await fs.stat(runtimeArchive)).size},null,2));
