import { useT } from "@/i18n";
import { ListingSwitch } from "../me/ListingSwitch";
import { MyDepartments } from "../me/MyDepartments";
import { MyQualifications } from "../me/MyQualifications";
import { MySchedule } from "../me/MySchedule";

/** `/volunteers/me`: my bookings, departments, qualifications and the playa-name listing choice. */
export function MyVolunteering() {
  const t = useT();
  return (
    <div className="flex flex-col gap-8">
      <h1 className="text-2xl font-bold">{t("module.volunteers.me.title")}</h1>
      <MySchedule />
      <MyDepartments />
      <MyQualifications />
      <ListingSwitch />
    </div>
  );
}
