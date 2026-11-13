-- Все тестовые изменения откатываются.
CREATE FUNCTION pg_temp.reject(statement text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    BEGIN
        EXECUTE statement;
    EXCEPTION WHEN integrity_constraint_violation THEN RETURN;
    END;
    RAISE EXCEPTION 'Ожидался отказ: %', statement;
END;
$$;
DO $$
DECLARE a uuid; c uuid; n integer;
BEGIN
    SELECT id INTO STRICT c FROM collaborators WHERE lastname = 'Проверка';
    INSERT INTO accounts(login,password_hash,role,updated_at)
        VALUES ('Admin','hash','admin','2000-01-01') RETURNING id INTO a;
    PERFORM pg_temp.reject($q$INSERT INTO accounts(login,password_hash,role) VALUES ('admin','hash','admin')$q$);
    PERFORM pg_temp.reject($q$INSERT INTO accounts(login,password_hash,role) VALUES ('staff','hash','employee')$q$);
    PERFORM pg_temp.reject($q$INSERT INTO accounts(login,password_hash,role) VALUES ('other','hash','unknown')$q$);
    PERFORM pg_temp.reject($q$INSERT INTO accounts(login,password_hash,role) VALUES (' bad ','hash','admin')$q$);
    PERFORM pg_temp.reject($q$INSERT INTO accounts(login,password_hash,role) VALUES ('valid','','admin')$q$);
    INSERT INTO accounts(login,password_hash,role,collaborator_id) VALUES ('staff','hash','employee',c);
    PERFORM pg_temp.reject(format('INSERT INTO accounts(login,password_hash,role,collaborator_id) VALUES (%L,%L,%L,%L)', 'duplicate','hash','admin',c));
    PERFORM pg_temp.reject(format('DELETE FROM collaborators WHERE id=%L',c));
    INSERT INTO auth_sessions(token_hash,account_id,expires_at)
        VALUES (decode(repeat('ab',32),'hex'),a,now()+interval '12 hours'),
               (decode(repeat('cd',32),'hex'),a,now()+interval '12 hours');
    PERFORM pg_temp.reject(format('INSERT INTO auth_sessions(token_hash,account_id,expires_at) VALUES (decode(repeat(''ab'',32),''hex''),%L,now()+interval ''1 hour'')',a));
    PERFORM pg_temp.reject(format('INSERT INTO auth_sessions(token_hash,account_id,expires_at) VALUES (decode(''ab'',''hex''),%L,now()+interval ''1 hour'')',a));
    PERFORM pg_temp.reject(format('INSERT INTO auth_sessions(token_hash,account_id,expires_at) VALUES (decode(repeat(''ef'',32),''hex''),%L,now())',a));
    UPDATE accounts SET login='administrator' WHERE id=a;
    IF NOT EXISTS (SELECT FROM accounts WHERE id=a AND updated_at > '2000-01-01') THEN
        RAISE EXCEPTION 'updated_at не обновлён';
    END IF;
    DELETE FROM accounts WHERE id=a;
    SELECT count(*) INTO n FROM auth_sessions WHERE account_id=a;
    IF n <> 0 THEN RAISE EXCEPTION 'Сессии не удалены каскадно'; END IF;
END;
$$;
