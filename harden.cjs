const fs=require('fs');const edit=(p,f)=>fs.writeFileSync(p,f(fs.readFileSync(p,'utf8').replace(/\r\n/g,'\n')));
edit('internal/deposit/repository.go',s=>s.replace('\trow := r.db.QueryRow(\n',` tx,err:=r.db.Begin(ctx);if err!=nil{return nil,err};defer tx.Rollback(ctx)
 row := tx.QueryRow(
`).replace('\treturn deposit, nil\n}\n\nfunc (r *Repository) ListByUser',` if provider==string(ProviderSandbox){deposit,err=r.completeTx(ctx,tx,provider,providerReference);if err!=nil{return nil,err}}
 if err:=tx.Commit(ctx);err!=nil{return nil,err}
 return deposit, nil
}

func (r *Repository) ListByUser`).replace('\tdefer tx.Rollback(ctx)\n\n\trow := tx.QueryRow(',` defer tx.Rollback(ctx)
 deposit,err:=r.completeTx(ctx,tx,provider,providerReference);if err!=nil{return nil,err}
 if err:=tx.Commit(ctx);err!=nil{return nil,err};return deposit,nil
}

func(r *Repository) completeTx(ctx context.Context,tx pgx.Tx,provider,providerReference string)(*Deposit,error){
 row := tx.QueryRow(`).replace(/\n\tif err := tx.Commit\(ctx\); err != nil \{\n\t\treturn nil, err\n\t}\n\n\treturn deposit, nil\n}\n$/,'\n\treturn deposit,nil\n}\n'));
