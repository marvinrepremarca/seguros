// Lo que parties necesita de otros módulos: app/ lo inyecta (un módulo no importa a otro).
export interface AgentOption {
  id: string;
  displayName: string;
  active: boolean;
}

export interface AgentsQuery {
  queryKey: readonly unknown[];
  queryFn: () => Promise<AgentOption[]>;
}
