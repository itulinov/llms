package main

// Поиск только по JSON-значениям: ключи, пароли и сессии не входят в документ.
func cardSearch(document, parameter string) string {
	return `EXISTS (SELECT FROM jsonb_path_query((` + document + `), 'strict $.** ? (@.type() != "object" && @.type() != "array")') AS search_leaf(value) WHERE (value #>> '{}') ILIKE ` + parameter + ` ESCAPE '\' OR (CASE WHEN (value #>> '{}') ~ '^\d{4}-\d{2}-\d{2}' THEN substring(value #>> '{}' FROM 9 FOR 2)||'.'||substring(value #>> '{}' FROM 6 FOR 2)||'.'||substring(value #>> '{}' FROM 1 FOR 4) WHEN value='true'::jsonb THEN 'да' WHEN value='false'::jsonb THEN 'нет' ELSE '' END) ILIKE ` + parameter + ` ESCAPE '\')`
}

const employeeSearchDocument = `to_jsonb(c) || jsonb_build_object('employment_label',CASE WHEN c.is_dismiss THEN 'Уволен' ELSE 'Работает' END,'cost_center',(SELECT name FROM cost_centers WHERE id=c.cost_center_id),'account',(SELECT jsonb_build_object('id',a.id,'login',a.login,'role',a.role,'role_label',CASE WHEN a.role='admin' THEN 'Администратор' ELSE 'Сотрудник' END,'state',CASE WHEN a.is_active THEN 'Активна' ELSE 'Заблокирована' END,'is_active',a.is_active) FROM accounts a WHERE a.collaborator_id=c.id),'positions',(SELECT coalesce(jsonb_agg(to_jsonb(p)||jsonb_build_object('subdivision',s.name,'org_id',o.id,'org',o.name,'common',pc.name)), '[]'::jsonb) FROM positions p JOIN subdivisions s ON s.id=p.subdivision_id JOIN orgs o ON o.id=s.org_id LEFT JOIN position_commons pc ON pc.id=p.position_common_id WHERE p.collaborator_id=c.id))`

func structureSearchDocument(table string) string {
	base := `to_jsonb(t)`
	if table != "orgs" {
		base += ` || jsonb_build_object('status_label',CASE t.status WHEN 'active' THEN 'Действует Активна' WHEN 'plan' THEN 'План' WHEN 'project' THEN 'Проект' WHEN 'close' THEN 'Закрыто' WHEN 'cancel' THEN 'Отменено' ELSE '' END)`
	}
	switch table {
	case "subdivisions":
		base += ` || jsonb_build_object('org',(SELECT name FROM orgs WHERE id=t.org_id),'parent',(SELECT name FROM subdivisions WHERE id=t.parent_id),'cost_center',(SELECT name FROM cost_centers WHERE id=t.cost_center_id))`
	case "positions":
		base += ` || jsonb_build_object('employee',(SELECT fullname FROM collaborators WHERE id=t.collaborator_id),'subdivision',(SELECT name FROM subdivisions WHERE id=t.subdivision_id),'org_id',(SELECT org_id FROM subdivisions WHERE id=t.subdivision_id),'org',(SELECT o.name FROM orgs o JOIN subdivisions s ON s.org_id=o.id WHERE s.id=t.subdivision_id),'common',(SELECT name FROM position_commons WHERE id=t.position_common_id),'occupancy',CASE WHEN t.collaborator_id IS NULL THEN 'Вакантна Вакантная' ELSE 'Занята' END)`
	}
	return base
}
