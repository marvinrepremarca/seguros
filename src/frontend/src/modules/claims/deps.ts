// Lo que claims necesita de otros módulos: app/ lo compone e inyecta.
export interface ClaimsDeps {
  policies: {
    queryKey: readonly unknown[];
    queryFn: () => Promise<{ id: string; label: string }[]>;
  };
}
