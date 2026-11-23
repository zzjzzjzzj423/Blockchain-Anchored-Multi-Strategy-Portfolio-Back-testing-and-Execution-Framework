"""Separate-party Ed25519 review protocol, bound to the v2 root and policy."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
from .market_data import verify_market_evidence
from .dataset import verify_inventory
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey,Ed25519PublicKey


def canonical(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()


def keygen(path):
    path=Path(path)
    key=Ed25519PrivateKey.generate()
    raw=key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption())
    with path.open('xb') as stream:stream.write(raw)
    path.chmod(0o600)
    public=key.public_key().public_bytes(serialization.Encoding.Raw,serialization.PublicFormat.Raw).hex()
    return public


def root_of(bundle):return json.loads((Path(bundle)/'merkle/merkle_manifest.json').read_text())['root']


def attest(bundle,policy_path,key_path,role,decision,comment):
    policy=json.loads(Path(policy_path).read_text())
    key=serialization.load_pem_private_key(Path(key_path).read_bytes(),password=None)
    public=key.public_key().public_bytes(serialization.Encoding.Raw,serialization.PublicFormat.Raw).hex()
    if public not in policy['roles'].get(role,[]):raise ValueError('signing key is not authorized for role')
    statement={'domain':'portfolio-evidence-review-v2','root':root_of(bundle),'policy_sha256':hashlib.sha256(canonical(policy)).hexdigest(),'role':role,'decision':decision,'comment':comment,'public_key':public}
    if role not in ('publisher','reviewer') or decision not in ('approve','reject'):raise ValueError('invalid role or decision')
    return {'statement':statement,'signature':key.sign(canonical(statement)).hex()}


def verify_reviews(bundle,policy_path,attestations):
    policy=json.loads(Path(policy_path).read_text());root=root_of(bundle)
    approved={};seen=set();rejected=False
    for entry in attestations:
        statement=entry['statement'];public=statement['public_key'];role=statement['role']
        if statement['domain']!='portfolio-evidence-review-v2' or statement['root']!=root:raise ValueError('review is bound to a different root')
        if statement['policy_sha256']!=hashlib.sha256(canonical(policy)).hexdigest():raise ValueError('review policy changed')
        if role not in ('publisher','reviewer') or public not in policy['roles'].get(role,[]):raise ValueError('unauthorized review signer')
        if public in seen:raise ValueError('same key cannot fulfill multiple parties or vote twice')
        seen.add(public)
        Ed25519PublicKey.from_public_bytes(bytes.fromhex(public)).verify(bytes.fromhex(entry['signature']),canonical(statement))
        if statement['decision']=='reject':rejected=True
        elif statement['decision']=='approve':approved[role]=approved.get(role,0)+1
        else:raise ValueError('invalid decision')
    passed=all(approved.get(role,0)>=1 for role in ('publisher','reviewer')) and not rejected
    return {'root':root,'state':'rejected' if rejected else 'approved' if passed else 'awaiting_review','approved_roles':approved,'distinct_keys':len(seen),'identity_scope':policy.get('identity_scope','external key ownership must be verified by participants')}


def main():
    p=argparse.ArgumentParser();sub=p.add_subparsers(dest='action',required=True)
    key=sub.add_parser('keygen');key.add_argument('--key',type=Path,required=True)
    sign=sub.add_parser('attest')
    for name in ('bundle','policy','key','output'):sign.add_argument('--'+name,type=Path,required=True)
    sign.add_argument('--role',choices=['publisher','reviewer'],required=True)
    sign.add_argument('--decision',choices=['approve','reject'],required=True)
    sign.add_argument('--comment',default='')
    sign.add_argument('--root',type=Path,default=Path('.'))
    sign.add_argument('--project',default='bin/project',help='Go verifier executable')
    check=sub.add_parser('verify')
    check.add_argument('--bundle',type=Path,required=True);check.add_argument('--policy',type=Path,required=True)
    check.add_argument('--attestations',type=Path,nargs='+',required=True)
    args=p.parse_args()
    if args.action=='keygen':print(keygen(args.key));return
    if args.action=='attest':
        subprocess.run([args.project,'evidence','verify-v2','--bundle',str(args.bundle)],check=True)
        if args.role=='reviewer' and args.decision=='approve':
            verify_market_evidence(args.bundle,args.root)
            if (args.bundle/'market_data/full_reference').exists():verify_inventory(args.bundle/'market_data/full_reference')
            subprocess.run([args.project,'experiment','agent-replay','--root',str(args.root),'--bundle',str(args.bundle)],check=True)
            for name in ('quality','cross_reference'):
                status=json.loads((args.bundle/'market_data'/(name+'.json')).read_text())['status']
                if status not in ('passed','corroborated'):raise ValueError('market data gate blocked')
        signed=attest(args.bundle,args.policy,args.key,args.role,args.decision,args.comment)
        with args.output.open('x') as stream:json.dump(signed,stream,sort_keys=True,indent=2)
        return
    result=verify_reviews(args.bundle,args.policy,[json.loads(path.read_text()) for path in args.attestations])
    print(json.dumps(result,sort_keys=True))
    if result['state']!='approved':raise SystemExit(1)


if __name__=='__main__':main()
