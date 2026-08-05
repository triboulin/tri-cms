// Dynamic field builder for schema creation/edition (Conception page).
// Renders/removes rows client-side; the server (pkg/api/htmx_conception.go)
// parses the resulting fields[N][...] form fields back into a
// pkg/schema.Definition on submit.

var TRI_FIELD_TYPES = [
  "Text", "RichText_MD", "RichText_HTML", "Float", "Int", "Date", "Media",
  "Boolean", "Enum", "Reference", "Slug", "URL", "Color", "JSON", "GeoPoint"
];

function triFieldRowHTML(index, values) {
  values = values || {};
  var schemaOptions = "<option value=\"\">(choisir un schéma)</option>";
  (window.triAvailableSchemas || []).forEach(function (slug) {
    var sel = values.targetSchema === slug ? " selected" : "";
    schemaOptions += "<option value=\"" + slug + "\"" + sel + ">" + slug + "</option>";
  });

  var typeOptions = "";
  TRI_FIELD_TYPES.forEach(function (t) {
    var sel = values.type === t ? " selected" : "";
    typeOptions += "<option value=\"" + t + "\"" + sel + ">" + t + "</option>";
  });

  var required = values.required ? " checked" : "";
  var cardCollection = values.cardinality === "Collection" ? " selected" : "";
  var cardSimple = values.cardinality === "Collection" ? "" : " selected";

  return (
    "<div class=\"tri-field-row\" data-index=\"" + index + "\">" +
    "<div><label>Clé</label><input name=\"fields[" + index + "][key]\" value=\"" + (values.key || "") + "\" placeholder=\"ex: title\" required></div>" +
    "<div><label>Libellé</label><input name=\"fields[" + index + "][label]\" value=\"" + (values.label || "") + "\" placeholder=\"Titre\"></div>" +
    "<div><label>Type</label><select name=\"fields[" + index + "][type]\" onchange=\"triToggleFieldExtra(this)\">" + typeOptions + "</select></div>" +
    "<div><label>Cardinalité</label><select name=\"fields[" + index + "][cardinality]\"><option value=\"Simple\"" + cardSimple + ">Simple</option><option value=\"Collection\"" + cardCollection + ">Collection</option></select></div>" +
    "<div><label>Requis</label><input type=\"checkbox\" name=\"fields[" + index + "][required]\" value=\"true\"" + required + "></div>" +
    "<div>" +
      "<div class=\"tri-field-extra\" data-role=\"options\"><label>Options (Enum, séparées par virgules)</label><input name=\"fields[" + index + "][options]\" value=\"" + (values.options || "") + "\" placeholder=\"draft,review,approved\"></div>" +
      "<div class=\"tri-field-extra\" data-role=\"target\"><label>Schéma cible (Reference)</label><select name=\"fields[" + index + "][targetSchema]\">" + schemaOptions + "</select></div>" +
      "<div class=\"tri-field-extra\" data-role=\"placeholder\"><label>Placeholder</label><input name=\"fields[" + index + "][placeholder]\" value=\"" + (values.placeholder || "") + "\"></div>" +
    "</div>" +
    "<div><button type=\"button\" class=\"tri-icon-btn danger\" title=\"Supprimer ce champ\" onclick=\"this.closest('.tri-field-row').remove()\"><span class=\"material-icons\">delete</span></button></div>" +
    "</div>"
  );
}

function triToggleFieldExtra(select) {
  var row = select.closest(".tri-field-row");
  var type = select.value;
  var optionsBox = row.querySelector('[data-role="options"]');
  var targetBox = row.querySelector('[data-role="target"]');
  var placeholderBox = row.querySelector('[data-role="placeholder"]');
  optionsBox.classList.toggle("show", type === "Enum");
  targetBox.classList.toggle("show", type === "Reference");
  placeholderBox.classList.toggle("show", type !== "Reference" && type !== "GeoPoint" && type !== "Boolean" && type !== "JSON");
}

function triAddFieldRow() {
  var builder = document.getElementById("fields-builder");
  var index = parseInt(builder.getAttribute("data-next-index"), 10) || 0;
  var wrapper = document.createElement("div");
  wrapper.innerHTML = triFieldRowHTML(index, {});
  var row = wrapper.firstChild;
  builder.appendChild(row);
  builder.setAttribute("data-next-index", index + 1);
  triToggleFieldExtra(row.querySelector("select"));
}

document.addEventListener("DOMContentLoaded", function () {
  document.querySelectorAll(".tri-field-row select").forEach(function (select) {
    triToggleFieldExtra(select);
  });
});
